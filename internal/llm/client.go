// Package llm is armin's client for chat models, over the
// OpenAI-compatible /v1/chat/completions that llama-server and hosted
// APIs (OpenRouter, Claude's) both speak: chat with native tool
// calling and streaming for the agent loop, and llama-server's
// /infill for inline completion. Standard library only.
//
//   client.go  Client: Chat, ChatStream (one request, streamed or
//              not), Chore (the same model, thinking off)
//   stream.go  reading replies; the idle timeout
//   wire.go    armin's Message ↔ the wire JSON; effort for a local
//              chat template
//   infill.go  Complete (fill-in-the-middle), Health, ContextSize
//
// One Client talks to one model; internal/llamacpp starts the servers
// and hands out Clients pointed at them.
package llm

import (
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "net/http"
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

// maxErrorBody caps how much of an error response is read: enough for
// any message, and a misbehaving server can't make armin hold more.
const maxErrorBody = 64 << 10

// maxReply caps a whole (non-streamed) reply: far more than any answer.
const maxReply = 16 << 20

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

// Chore returns c for background work (summaries, saving notes,
// suggestions) where thinking buys little: a hosted reasoning model
// gets a copy with reasoning off, since on a long context its
// thinking can take longer than the answer. A local llama-server
// gets c itself.
func (c *Client) Chore() *Client {
  if c.APIKey == "" {
    return c
  }

  q := *c
  q.Effort = "none"

  return &q
}

// ChatStream is Chat with separate handlers for content and
// reasoning deltas. Tool-call argument fragments arrive
// incrementally too; readStreamedReply assembles them by index.
//
// The flow: wait until the model's server is up (Ready, set by
// llamacpp for a model it starts), build the request (request), post
// it under an idle timeout (a stream that keeps arriving is never cut
// off), retry once on the global host when a regional one refuses
// (403), and read the reply, streamed or whole, into one Message.
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

  reqBody := c.request(messages, tools, h)

  body, err := json.Marshal(reqBody)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ChatStream:\n\t marshaling chat request: %w", err,
    )
  }

  parent := ctx
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

  if resp.StatusCode == http.StatusForbidden && c.Fallback != "" {
    from := c.BaseURL
    c.BaseURL, c.Fallback = c.Fallback, ""
    if c.OnFallback != nil {
      c.OnFallback(from, c.BaseURL)
    }

    return c.ChatStream(parent, messages, tools, h)
  }

  if resp.StatusCode != http.StatusOK {
    respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
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

// request is the wire body for a chat call: the history and tools in
// the OpenAI shape, the model's id, the effort (OpenRouter's field for
// a hosted API, the template's switches for llama-server) and, when
// streaming, usage and prompt-progress reports.
func (c *Client) request(
  messages []Message, tools []ToolDefinition, h StreamHandlers,
) owaiChatRequest {
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
    Tools:     wireTools,
    Stream:    h.streaming(),
    MaxTokens: c.MaxTokens,
  }

  // A hosted API (an id or a key of its own) takes OpenRouter's
  // field; llama-server takes its template's.
  hosted := c.APIKey != "" || c.ID != ""
  if c.Effort != "" && hosted {
    reqBody.Reasoning = &owaiReasoning{ Effort: c.Effort }
  } else if c.Effort != "" {
    reqBody.TemplateKwargs, reqBody.ThinkBudget = localEffort(c.Effort)
  }

  if c.Private && hosted {
    reqBody.Provider = &privateProvider
  }

  if reqBody.Stream {
    reqBody.StreamOptions = &owaiStreamOptions{ IncludeUsage: true }
    // Only for llama-server (no API key): a hosted API may reject a
    // field it doesn't know.
    reqBody.ReturnProgress = h.Progress != nil && c.APIKey == ""
  }

  return reqBody
}


// privateProvider is Client.Private's routing: shared, never written.
var privateProvider = owaiProvider{ DataCollection: "deny", ZDR: true }

// infillSamplers is llama.vim's chain: top-k/top-p narrow, the
// infill sampler favors ending cleanly at the suffix.
var infillSamplers = []string{ "top_k", "top_p", "infill" }

// --- OpenAI-compatible wire shapes, private to this package -------
//
// llama-server's chat endpoint mirrors OpenAI's Chat Completions
// API, which differs from the Message/ToolCall shape the rest of
// armin uses in two ways this package bridges at its edges: a tool
// call's arguments travel as a JSON-encoded *string*, not a native
// object, and every tool call carries an id that a following
// role:"tool" message must echo back as tool_call_id.

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

