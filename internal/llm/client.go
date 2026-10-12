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

// Total is the context the conversation occupied after this reply.
func (u Usage) Total() int { return u.PromptTokens + u.CompletionTokens }

// DefaultReadTimeout covers prompt processing of a large context on
// a remote box before the first byte arrives.
const DefaultReadTimeout = 5 * time.Minute

// New returns a Client pointed at a local llama-server instance.
func New(baseURL, model string) *Client {
  if baseURL == "" {
    baseURL = "http://localhost:8080"
  }

  return &Client{
    BaseURL: baseURL,
    Model:   model,
    HTTP:    &http.Client{},
  }
}

func (h StreamHandlers) streaming() bool {
  return h.Token != nil || h.Reasoning != nil
}

// setAuthHeader adds the Authorization header when APIKey is set —
// a no-op for the common local-llama-server case, where it's empty.
func (c *Client) setAuthHeader(req *http.Request) {
  if c.APIKey != "" {
    req.Header.Set("Authorization", "Bearer " + c.APIKey)
  }
}

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
// callers must resend the whole history every turn. A non-nil
// onToken streams content deltas; see ChatStream for reasoning too.
func (c *Client) Chat(
  ctx context.Context, messages []Message,
  tools []ToolDefinition, onToken OnToken,
) (*Message, error) {
  return c.ChatStream(
    ctx, messages, tools, StreamHandlers{ Token: onToken },
  )
}

// ChatStream is Chat with separate handlers for content and
// reasoning deltas. Tool-call argument fragments arrive
// incrementally too; readStreamedReply assembles them by index.
func (c *Client) ChatStream(
  ctx context.Context, messages []Message,
  tools []ToolDefinition, h StreamHandlers,
) (*Message, error) {
  if c.Ready != nil {
    err := c.Ready(ctx)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in ChatStream:\n\t%w", err)
    }
  }

  defer c.requestBracket()()

  wireTools := make([]owaiTool, 0, len(tools))
  for _, t := range tools {
    wireTools = append(
      wireTools, owaiTool{ Type: "function", Function: t.Function },
    )
  }

  model := c.Model
  if c.ID != "" {
    model = c.ID
  }

  reqBody := owaiChatRequest{
    Model:    model,
    Messages: toWireMessages(messages),
    Tools:    wireTools,
    Stream:   h.streaming(),
  }

  if c.Effort != "" {
    reqBody.Reasoning = &owaiReasoning{ Effort: c.Effort }
  }

  if reqBody.Stream {
    reqBody.StreamOptions = &owaiStreamOptions{ IncludeUsage: true }
    // Only for llama-server (no API key): a hosted API may reject a
    // field it doesn't know.
    reqBody.ReturnProgress = h.Progress != nil && c.APIKey == ""
  }

  body, err := json.Marshal(reqBody)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ChatStream:\n\t marshaling chat request: %w", err,
    )
  }

  ctx, idle := c.watchIdle(ctx)
  defer idle.stop()

  url := c.endpoint("/v1/chat/completions")
  req, err := http.NewRequestWithContext(
    ctx, http.MethodPost, url, bytes.NewReader(body),
  )
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ChatStream:\n\t building request: %w", err,
    )
  }

  req.Header.Set("Content-Type", "application/json")
  c.setAuthHeader(req)

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ChatStream:\n\t calling llama-server at %s: %w",
      c.BaseURL, idle.explain(err),
    )
  }

  defer drainClose(resp.Body)
  idle.touch()

  if resp.StatusCode != http.StatusOK {
    respBody, _ := io.ReadAll(resp.Body)
    if looksLikeToolsUnsupported(respBody) {
      return nil, fmt.Errorf(
        "\n\t... in ChatStream:\n\t %w: %s",
        ErrToolsUnsupported, c.Model,
      )
    }

    return nil, fmt.Errorf(
      "\n\t... in ChatStream:\n\t llama-server returned %d: %s",
      resp.StatusCode, string(respBody),
    )
  }

  r := idleReader{ r: resp.Body, w: idle }
  var msg *Message
  if reqBody.Stream {
    msg, err = readStreamedReply(r, h)
  } else {
    msg, err = readWholeReply(r)
  }

  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ChatStream:\n\t%w", idle.explain(err),
    )
  }

  return msg, nil
}

// drainClose reads what's left of a body (bounded) before closing
// it: Go's transport only reuses a keep-alive connection whose body
// was read to EOF. Over a remote link that saves a TCP+TLS
// handshake per request.
func drainClose(body io.ReadCloser) {
  io.Copy(io.Discard, io.LimitReader(body, 64 << 10))
  body.Close()
}

func (c *Client) watchIdle(
  ctx context.Context,
) (context.Context, *idleWatch) {
  after := c.ReadTimeout
  if after <= 0 {
    after = DefaultReadTimeout
  }

  ctx, cancel := context.WithCancel(ctx)
  w := &idleWatch{ after: after, expired: make(chan struct{}) }
  w.timer = time.AfterFunc(after, func() {
    close(w.expired)
    cancel()
  })

  return ctx, w
}

func (w *idleWatch) touch() { w.timer.Reset(w.after) }
func (w *idleWatch) stop()  { w.timer.Stop() }

// explain swaps a bare "context canceled" for the real reason when
// the idle timer is what fired.
func (w *idleWatch) explain(err error) error {
  select {
  case <-w.expired:
    return fmt.Errorf(
      "no data from the server for %s (read timeout): %w",
      w.after, err,
    )
  default:
    return err
  }
}

func (ir idleReader) Read(p []byte) (int, error) {
  n, err := ir.r.Read(p)
  if n > 0 {
    ir.w.touch()
  }

  return n, err
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
  err = json.Unmarshal(body, &parsed)
  if err != nil {
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

  if parsed.Usage != nil {
    msg.Usage = *parsed.Usage
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
var (
  sseData = []byte("data:")
  sseDone = []byte("[DONE]")
)

func readStreamedReply(
  r io.Reader, h StreamHandlers,
) (*Message, error) {
  scanner := bufio.NewScanner(r)
  scanner.Buffer(make([]byte, 0, 64 * 1024), 1024 * 1024)

  var content, reasoning strings.Builder
  var usage Usage
  calls := map[int]*toolCallAccumulator{}
  var order []int

  for scanner.Scan() {
    // Bytes, not Text: no string allocation per SSE line.
    data, ok := bytes.CutPrefix(scanner.Bytes(), sseData)
    if !ok {
      continue // blank line, "event:" field, or SSE comment — none used here
    }

    data = bytes.TrimSpace(data)
    if len(data) == 0 {
      continue
    }

    if bytes.Equal(data, sseDone) {
      break
    }

    var chunk owaiStreamChunk
    err := json.Unmarshal(data, &chunk)
    if err != nil {
      return nil, fmt.Errorf(
        "\n\t... in readStreamedReply:\n\t parsing chunk: %w (line: %s)",
        err, data,
      )
    }

    if chunk.Usage != nil {
      usage = *chunk.Usage
    }

    p := chunk.Progress
    if p != nil && h.Progress != nil {
      h.Progress(p.Processed, p.Total)
    }

    if len(chunk.Choices) == 0 {
      continue
    }

    delta := chunk.Choices[0].Delta
    if delta.ReasoningContent != "" {
      reasoning.WriteString(delta.ReasoningContent)
      if h.Reasoning != nil {
        h.Reasoning(delta.ReasoningContent)
      }
    }

    if delta.Content != "" {
      content.WriteString(delta.Content)
      if h.Token != nil {
        h.Token(delta.Content)
      }
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

  err := scanner.Err()
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readStreamedReply:\n\t streaming llama-server response: %w",
      err,
    )
  }

  final := Message{
    Role:      "assistant",
    Content:   content.String(),
    Reasoning: reasoning.String(),
    Usage:     usage,
  }

  sort.Ints(order)
  for _, idx := range order {
    acc := calls[idx]

    var args map[string]any
    if acc.arguments.Len() > 0 {
      err := json.Unmarshal([]byte(acc.arguments.String()), &args)
      if err != nil {
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

// infillSamplers is llama.vim's chain: top-k/top-p narrow, the
// infill sampler favors ending cleanly at the suffix.
var infillSamplers = []string{ "top_k", "top_p", "infill" }

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
  if c.Ready != nil {
    err := c.Ready(ctx)
    if err != nil {
      return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
    }
  }

  defer c.requestBracket()()

  fill := infillRequest{
    InputPrefix: prefix,
    InputSuffix: suffix,
    InputExtra:  opts.Extra,
    Prompt:      opts.Prompt,
    NPredict:    opts.NumPredict,
    NIndent:     opts.Indent,
    TMaxPredict: opts.MaxPredictMs,
    Temperature: opts.Temperature,
    Stop:        opts.Stop,
    CachePrompt: true,
  }
  if opts.TopK > 0 {
    fill.TopK, fill.TopP, fill.Samplers = opts.TopK, opts.TopP, infillSamplers
  }

  body, err := json.Marshal(fill)
  if err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }

  ctx, idle := c.watchIdle(ctx)
  defer idle.stop()

  url := c.BaseURL + "/infill"
  req, err := http.NewRequestWithContext(
    ctx, http.MethodPost, url, bytes.NewReader(body),
  )
  if err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }

  req.Header.Set("Content-Type", "application/json")
  c.setAuthHeader(req)

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return "", fmt.Errorf(
      "\n\t... in Complete:\n\t calling llama-server at %s: %w",
      c.BaseURL, idle.explain(err),
    )
  }

  defer resp.Body.Close()

  respBody, err := io.ReadAll(idleReader{ r: resp.Body, w: idle })
  if err != nil {
    return "", fmt.Errorf(
      "\n\t... in Complete:\n\t%w", idle.explain(err),
    )
  }

  if resp.StatusCode != http.StatusOK {
    return "", fmt.Errorf(
      "\n\t... in Complete:\n\t llama-server returned %d: %s",
      resp.StatusCode, string(respBody),
    )
  }

  var parsed infillResponse
  err = json.Unmarshal(respBody, &parsed)
  if err != nil {
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

  c.setAuthHeader(req)

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

// ContextSize asks llama-server's /props for the per-slot context
// window. Zero with a nil error means the server didn't say (a
// remote OpenAI-compatible endpoint has no /props at all).
func (c *Client) ContextSize(ctx context.Context) (int, error) {
  req, err := http.NewRequestWithContext(
    ctx, http.MethodGet, c.BaseURL + "/props", nil,
  )
  if err != nil {
    return 0, fmt.Errorf("\n\t... in ContextSize:\n\t%w", err)
  }

  c.setAuthHeader(req)

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return 0, fmt.Errorf("\n\t... in ContextSize:\n\t%w", err)
  }

  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    return 0, nil
  }

  var p propsResponse
  err = json.NewDecoder(resp.Body).Decode(&p)
  if err != nil {
    return 0, nil
  }

  if p.DefaultGenerationSettings.NCtx > 0 {
    return p.DefaultGenerationSettings.NCtx, nil
  }

  return p.NCtx, nil
}

// --- OpenAI-compatible wire shapes, private to this package -------
//
// llama-server's chat endpoint mirrors OpenAI's Chat Completions
// API, which differs from the Message/ToolCall shape the rest of
// armin uses in two ways this package bridges at its edges: a tool
// call's arguments travel as a JSON-encoded *string*, not a native
// object, and every tool call carries an id that a following
// role:"tool" message must echo back as tool_call_id.

// toWireMessages translates outgoing history to the OpenAI shape —
// each ToolCall's map[string]any Arguments becomes a JSON-encoded
// string, and ToolCallID is carried straight through so a
// role:"tool" message stays matched to the call that produced it.
func toWireMessages(msgs []Message) []owaiMessage {
  out := make([]owaiMessage, len(msgs))
  for i, m := range msgs {
    wm := owaiMessage{
      Role:             m.Role,
      Content:          m.Content,
      ReasoningContent: m.Reasoning,
      ToolCallID:       m.ToolCallID,
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

  m := Message{
    Role:      role,
    Content:   wm.Content,
    Reasoning: wm.ReasoningContent,
  }

  for i, tc := range wm.ToolCalls {
    var args map[string]any
    if tc.Function.Arguments != "" {
      err := json.Unmarshal([]byte(tc.Function.Arguments), &args)
      if err != nil {
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

// endpoint joins an OpenAI-style path onto BaseURL, which may or may
// not end in /v1 already ("https://api.anthropic.com/v1",
// "https://openrouter.ai/api").
func (c *Client) endpoint(path string) string {
  base := strings.TrimSuffix(c.BaseURL, "/")
  if strings.HasSuffix(base, "/v1") && strings.HasPrefix(path, "/v1/") {
    return base + path[3:]
  }

  return base + path
}
