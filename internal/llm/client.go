// Package llm is a minimal client for a locally running llama.cpp
// llama-server instance: chat with native tool calling and token
// streaming for the agent loop over its OpenAI-compatible
// /v1/chat/completions, and a separate /infill call for
// latency-sensitive inline code completion — no MCP involved in
// this client, see plan section 1. One process serves exactly one
// loaded model (unlike Ollama's multi-model daemon this replaced);
// see internal/llamacpp for the layer that spawns/reaps llama-server
// processes and hands out Clients pointed at the right one.
package llm

import (
  "bufio"
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "net/http"
  "sort"
  "strings"
  "time"
)

// ErrToolsUnsupported means the loaded model/chat-template rejected
// the request because it has no tool-calling support at all — no
// prompt or retry fixes this, only switching to a different model
// does. Detection is best-effort text matching against llama.cpp's
// error body (its wording isn't a stable contract, same caveat this
// had against Ollama's wording before). Callers match it with
// errors.Is to show a clean, actionable message instead of the raw
// HTTP error.
var ErrToolsUnsupported = errors.New("model does not support tool calling")

// Message is armin's own message shape, independent of the wire
// format any particular backend wants — toWireMessages/
// fromWireMessage translate to and from llama-server's
// OpenAI-compatible JSON at the edges of this package. Role is one
// of "system", "user", "assistant", or "tool".
type Message struct {
  Role      string     `json:"role"`
  Content   string     `json:"content"`
  ToolCalls []ToolCall `json:"tool_calls,omitempty"`
  // ToolName is set on role:"tool" messages so a human-readable log
  // doesn't need to cross-reference ToolCallID against the
  // assistant message that issued it.
  ToolName string `json:"tool_name,omitempty"`
  // ToolCallID echoes back the ToolCall.ID that produced this
  // role:"tool" message's result — required by the OpenAI-shaped
  // wire protocol to disambiguate results when a model requests
  // more than one tool call in the same turn.
  ToolCallID string `json:"tool_call_id,omitempty"`
}

// ToolCall is a single tool invocation the model asked for. ID
// comes from the model when it supplies one; fromWireMessage
// synthesizes a positional one ("call_0", "call_1", ...) when it
// doesn't, so callers can always rely on it being non-empty.
type ToolCall struct {
  ID       string           `json:"id"`
  Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
  Name      string         `json:"name"`
  Arguments map[string]any `json:"arguments"`
}

// ToolDefinition describes one callable tool using the JSON-schema
// shape OpenAI-style tool calling (and so llama.cpp's
// OpenAI-compatible endpoint) expects. Keep Description short —
// small models are sensitive to prompt bloat, and tool schemas
// share the same context budget as the conversation.
type ToolDefinition struct {
  Type     string       `json:"type"` // always "function"
  Function ToolFunction `json:"function"`
}

type ToolFunction struct {
  Name        string         `json:"name"`
  Description string         `json:"description"`
  Parameters  ToolParameters `json:"parameters"`
}

type ToolParameters struct {
  Type       string                  `json:"type"` // "object"
  Properties map[string]ToolProperty `json:"properties"`
  Required   []string                `json:"required,omitempty"`
}

type ToolProperty struct {
  Type        string `json:"type"`
  Description string `json:"description,omitempty"`
}

// Client is a minimal HTTP client for one already-running
// llama-server instance.
type Client struct {
  BaseURL string
  // Model is a display label only — llama-server ignores the
  // "model" field in requests and always answers with whatever
  // .gguf it was started with. It's still sent, since the
  // OpenAI-compatible endpoint expects the field to be present, and
  // used locally for session labels and REPL/LSP display.
  //
  // BaseURL/Model are mutated in place by internal/llamacpp's
  // Manager.Switch on a /model (or armin.model) switch, rather than
  // this Client being replaced wholesale — callers across the REPL,
  // agent.Loop, and lspserver all hold the same *Client pointer, so
  // in-place mutation is what makes a switch visible everywhere at
  // once. That mutation isn't synchronized against a concurrent
  // in-flight request (a pre-existing property of this pattern, not
  // new here) — a /model switch racing a request already in flight
  // on the same Client can send that request to the new BaseURL
  // mid-call.
  Model string
  HTTP  *http.Client

  // OnRequestStart/OnRequestEnd, if set, bracket every Chat/Complete
  // HTTP round trip (started before the request, ended via defer
  // once it returns). internal/llamacpp's Manager wires these to
  // know when a spawned model process is actually in use, so its
  // idle reaper never stops one mid-request. nil-safe: a zero-value
  // Client just skips them.
  OnRequestStart func()
  OnRequestEnd   func()
}

// New returns a Client pointed at a local llama-server instance.
func New(baseURL, model string) *Client {
  if baseURL == "" {
    baseURL = "http://localhost:8080"
  }
  return &Client{
    BaseURL: baseURL,
    Model:   model,
    HTTP:    &http.Client{ Timeout: 5 * time.Minute },
  }
}

// OnToken is called once per content delta as the model streams a
// reply. It's nil-safe to skip: pass nil to Chat and it behaves
// like a plain non-streaming call.
type OnToken func(delta string)

func (c *Client) requestBracket() func() {
  if c.OnRequestStart != nil {
    c.OnRequestStart()
  }
  return func() {
    if c.OnRequestEnd != nil {
      c.OnRequestEnd()
    }
  }
}

// Chat sends the full message history plus tool definitions and
// returns the model's next message (which may itself contain tool
// calls). llama-server has no server-side conversation state, so
// callers must resend the whole history every turn. When onToken is
// non-nil, content deltas are streamed to it as they arrive over
// server-sent events; tool-call argument fragments can also arrive
// incrementally in the stream (unlike Ollama, which only ever sent
// them complete on the final chunk) — readStreamedReply accumulates
// them by index before returning.
func (c *Client) Chat(
  ctx context.Context, messages []Message,
  tools []ToolDefinition, onToken OnToken,
) (*Message, error) {
  defer c.requestBracket()()

  wireTools := make([]owaiTool, 0, len(tools))
  for _, t := range tools {
    wireTools = append(
      wireTools, owaiTool{ Type: "function", Function: t.Function },
    )
  }

  reqBody := owaiChatRequest{
    Model:    c.Model,
    Messages: toWireMessages(messages),
    Tools:    wireTools,
    Stream:   onToken != nil,
  }

  body, err := json.Marshal(reqBody)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Chat:\n\t marshaling chat request: %w", err,
    )
  }

  url := c.BaseURL + "/v1/chat/completions"
  req, err := http.NewRequestWithContext(
    ctx, http.MethodPost, url, bytes.NewReader(body),
  )
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Chat:\n\t building request: %w", err,
    )
  }
  req.Header.Set("Content-Type", "application/json")

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Chat:\n\t calling llama-server at %s: %w",
      c.BaseURL, err,
    )
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    respBody, _ := io.ReadAll(resp.Body)
    if looksLikeToolsUnsupported(respBody) {
      return nil, fmt.Errorf(
        "\n\t... in Chat:\n\t %w: %s", ErrToolsUnsupported, c.Model,
      )
    }
    return nil, fmt.Errorf(
      "\n\t... in Chat:\n\t llama-server returned %d: %s",
      resp.StatusCode, string(respBody),
    )
  }

  if onToken == nil {
    return readWholeReply(resp.Body)
  }
  return readStreamedReply(resp.Body, onToken)
}

// looksLikeToolsUnsupported is a best-effort heuristic, not a
// stable contract: llama.cpp's error wording for "this model/
// template can't do tool calling" varies by version and isn't
// guaranteed to stay the same.
func looksLikeToolsUnsupported(body []byte) bool {
  s := strings.ToLower(string(body))
  if !strings.Contains(s, "tool") {
    return false
  }
  return strings.Contains(s, "not support") ||
    strings.Contains(s, "unsupported") ||
    strings.Contains(s, "not callable") ||
    strings.Contains(s, "no tool")
}

func readWholeReply(r io.Reader) (*Message, error) {
  body, err := io.ReadAll(r)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readWholeReply:\n\t reading llama-server response: %w",
      err,
    )
  }

  var parsed owaiChatResponse
  if err := json.Unmarshal(body, &parsed); err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readWholeReply:\n\t parsing llama-server response: %w (body: %s)",
      err, string(body),
    )
  }
  if parsed.Error != nil {
    return nil, fmt.Errorf(
      "\n\t... in readWholeReply:\n\t llama-server error: %s",
      parsed.Error.Message,
    )
  }
  if len(parsed.Choices) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in readWholeReply:\n\t no choices in llama-server response (body: %s)",
      string(body),
    )
  }

  msg, err := fromWireMessage(parsed.Choices[0].Message)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in readWholeReply:\n\t%w", err)
  }
  return &msg, nil
}

// readStreamedReply consumes llama-server's server-sent-events
// stream ("data: {...}" lines, terminated by "data: [DONE]"),
// forwarding each content delta to onToken as it arrives, and
// accumulating tool-call argument fragments by their delta index
// (OpenAI-shaped streaming can split one tool call's arguments
// across many chunks, unlike Ollama's always-whole-on-final-chunk
// shape) before returning the fully assembled message.
func readStreamedReply(r io.Reader, onToken OnToken) (*Message, error) {
  scanner := bufio.NewScanner(r)
  scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

  var content strings.Builder
  calls := map[int]*toolCallAccumulator{}
  var order []int

  for scanner.Scan() {
    line := scanner.Text()
    data, ok := strings.CutPrefix(line, "data:")
    if !ok {
      continue // blank line, "event:" field, or SSE comment — none used here
    }
    data = strings.TrimSpace(data)
    if data == "" {
      continue
    }
    if data == "[DONE]" {
      break
    }

    var chunk owaiStreamChunk
    if err := json.Unmarshal([]byte(data), &chunk); err != nil {
      return nil, fmt.Errorf(
        "\n\t... in readStreamedReply:\n\t parsing chunk: %w (line: %s)",
        err, data,
      )
    }
    if len(chunk.Choices) == 0 {
      continue
    }

    delta := chunk.Choices[0].Delta
    if delta.Content != "" {
      content.WriteString(delta.Content)
      onToken(delta.Content)
    }

    for _, tc := range delta.ToolCalls {
      acc, seen := calls[tc.Index]
      if !seen {
        acc = &toolCallAccumulator{}
        calls[tc.Index] = acc
        order = append(order, tc.Index)
      }
      if tc.ID != "" {
        acc.id = tc.ID
      }
      if tc.Function.Name != "" {
        acc.name = tc.Function.Name
      }
      acc.arguments.WriteString(tc.Function.Arguments)
    }
  }

  if err := scanner.Err(); err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readStreamedReply:\n\t streaming llama-server response: %w",
      err,
    )
  }

  final := Message{ Role: "assistant", Content: content.String() }

  sort.Ints(order)
  for _, idx := range order {
    acc := calls[idx]

    var args map[string]any
    if acc.arguments.Len() > 0 {
      if err := json.Unmarshal([]byte(acc.arguments.String()), &args); err != nil {
        return nil, fmt.Errorf(
          "\n\t... in readStreamedReply:\n\t parsing streamed tool call arguments: %w (raw: %s)",
          err, acc.arguments.String(),
        )
      }
    }

    id := acc.id
    if id == "" {
      id = fmt.Sprintf("call_%d", idx)
    }
    final.ToolCalls = append(final.ToolCalls, ToolCall{
      ID: id,
      Function: ToolCallFunction{
        Name: acc.name, Arguments: args,
      },
    })
  }

  return &final, nil
}

type toolCallAccumulator struct {
  id        string
  name      string
  arguments strings.Builder
}

// CompleteOptions tunes one Complete call. Unlike the Ollama-backed
// version this replaced, there's no per-call Model override:
// picking a different model means calling a different Client
// pointed at that model's own llama-server process (see
// internal/llamacpp.Manager), not a field on this request.
type CompleteOptions struct {
  Temperature float64
  NumPredict  int // cap on generated tokens — this is a completion, not an essay
  Stop        []string
}

type infillRequest struct {
  InputPrefix string   `json:"input_prefix"`
  InputSuffix string   `json:"input_suffix"`
  Prompt      string   `json:"prompt"` // required by /infill even when empty — no mid-prompt insertion here
  NPredict    int      `json:"n_predict,omitempty"`
  Temperature float64  `json:"temperature"`
  Stop        []string `json:"stop,omitempty"`
  Stream      bool     `json:"stream"`
}

type infillResponse struct {
  Content string `json:"content"`
}

// Complete asks the loaded fill-in-the-middle-capable model to
// continue prefix with suffix (the code already after the cursor)
// in view, via llama-server's native /infill endpoint — built
// exactly for this, so (unlike Ollama's /api/generate+suffix
// concatenation this replaced) the server applies the model's own
// FIM special tokens rather than armin gluing prefix/suffix into one
// prompt string. This is the latency-sensitive inline-completion
// path; it never touches the agent loop.
func (c *Client) Complete(
  ctx context.Context, prefix, suffix string, opts CompleteOptions,
) (string, error) {
  defer c.requestBracket()()

  body, err := json.Marshal(infillRequest{
    InputPrefix: prefix,
    InputSuffix: suffix,
    NPredict:    opts.NumPredict,
    Temperature: opts.Temperature,
    Stop:        opts.Stop,
    Stream:      false,
  })
  if err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }

  url := c.BaseURL + "/infill"
  req, err := http.NewRequestWithContext(
    ctx, http.MethodPost, url, bytes.NewReader(body),
  )
  if err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }
  req.Header.Set("Content-Type", "application/json")

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return "", fmt.Errorf(
      "\n\t... in Complete:\n\t calling llama-server at %s: %w",
      c.BaseURL, err,
    )
  }
  defer resp.Body.Close()

  respBody, err := io.ReadAll(resp.Body)
  if err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }
  if resp.StatusCode != http.StatusOK {
    return "", fmt.Errorf(
      "\n\t... in Complete:\n\t llama-server returned %d: %s",
      resp.StatusCode, string(respBody),
    )
  }

  var parsed infillResponse
  if err := json.Unmarshal(respBody, &parsed); err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }
  return parsed.Content, nil
}

// Health reports whether the llama-server instance at BaseURL is up
// and has finished loading its model — internal/llamacpp's Manager
// polls this after spawning a managed process, before handing the
// Client to a caller.
func (c *Client) Health(ctx context.Context) error {
  url := c.BaseURL + "/health"
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
  if err != nil {
    return fmt.Errorf("\n\t... in Health:\n\t%w", err)
  }

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return fmt.Errorf("\n\t... in Health:\n\t%w", err)
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    body, _ := io.ReadAll(resp.Body)
    return fmt.Errorf(
      "\n\t... in Health:\n\t llama-server returned %d: %s",
      resp.StatusCode, string(body),
    )
  }
  return nil
}

// --- OpenAI-compatible wire shapes, private to this package -------
//
// llama-server's chat endpoint mirrors OpenAI's Chat Completions
// API, which differs from the Message/ToolCall shape the rest of
// armin uses in two ways this package bridges at its edges: a tool
// call's arguments travel as a JSON-encoded *string*, not a native
// object, and every tool call carries an id that a following
// role:"tool" message must echo back as tool_call_id.

type owaiTool struct {
  Type     string       `json:"type"` // "function"
  Function ToolFunction `json:"function"`
}

type owaiToolCall struct {
  ID       string           `json:"id"`
  Type     string           `json:"type"` // "function"
  Function owaiToolCallFunc `json:"function"`
}

type owaiToolCallFunc struct {
  Name      string `json:"name"`
  Arguments string `json:"arguments"` // JSON-encoded object
}

type owaiMessage struct {
  Role       string         `json:"role"`
  Content    string         `json:"content"`
  ToolCalls  []owaiToolCall `json:"tool_calls,omitempty"`
  ToolCallID string         `json:"tool_call_id,omitempty"`
}

type owaiChatRequest struct {
  Model    string        `json:"model"`
  Messages []owaiMessage `json:"messages"`
  Tools    []owaiTool    `json:"tools,omitempty"`
  Stream   bool          `json:"stream"`
}

type owaiChoice struct {
  Message owaiMessage `json:"message"`
}

type owaiError struct {
  Message string `json:"message"`
  Type    string `json:"type"`
}

type owaiChatResponse struct {
  Choices []owaiChoice `json:"choices"`
  Error   *owaiError   `json:"error,omitempty"`
}

type owaiStreamToolCall struct {
  Index    int              `json:"index"`
  ID       string           `json:"id,omitempty"`
  Function owaiToolCallFunc `json:"function"`
}

type owaiStreamDelta struct {
  Content   string               `json:"content,omitempty"`
  ToolCalls []owaiStreamToolCall `json:"tool_calls,omitempty"`
}

type owaiStreamChoice struct {
  Delta owaiStreamDelta `json:"delta"`
}

type owaiStreamChunk struct {
  Choices []owaiStreamChoice `json:"choices"`
}

// toWireMessages translates outgoing history to the OpenAI shape —
// each ToolCall's map[string]any Arguments becomes a JSON-encoded
// string, and ToolCallID is carried straight through so a
// role:"tool" message stays matched to the call that produced it.
func toWireMessages(msgs []Message) []owaiMessage {
  out := make([]owaiMessage, len(msgs))
  for i, m := range msgs {
    wm := owaiMessage{
      Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID,
    }
    for _, tc := range m.ToolCalls {
      args, err := json.Marshal(tc.Function.Arguments)
      if err != nil {
        args = []byte("{}")
      }
      wm.ToolCalls = append(wm.ToolCalls, owaiToolCall{
        ID: tc.ID, Type: "function",
        Function: owaiToolCallFunc{
          Name: tc.Function.Name, Arguments: string(args),
        },
      })
    }
    out[i] = wm
  }
  return out
}

// fromWireMessage translates one incoming OpenAI-shaped message
// back to armin's own Message — parsing each tool call's
// JSON-encoded arguments string back into a map, and synthesizing a
// positional ID ("call_0", "call_1", ...) for any tool call the
// model didn't supply one for.
func fromWireMessage(wm owaiMessage) (Message, error) {
  role := wm.Role
  if role == "" {
    role = "assistant"
  }
  m := Message{ Role: role, Content: wm.Content }

  for i, tc := range wm.ToolCalls {
    var args map[string]any
    if tc.Function.Arguments != "" {
      if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
        return Message{}, fmt.Errorf(
          "parsing tool call arguments for %q: %w",
          tc.Function.Name, err,
        )
      }
    }
    id := tc.ID
    if id == "" {
      id = fmt.Sprintf("call_%d", i)
    }
    m.ToolCalls = append(m.ToolCalls, ToolCall{
      ID: id,
      Function: ToolCallFunction{
        Name: tc.Function.Name, Arguments: args,
      },
    })
  }
  return m, nil
}
