// The Client and the message shapes: armin's own (Message,
// ToolCall, ToolDefinition) and the wire's (owai*).

package llm

import (
  "context"
  "io"
  "net/http"
  "strings"
  "time"
)

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
  // Reasoning is the model's thinking (llama-server's
  // reasoning_content). Sent back within a turn so thinking models
  // keep their chain across tool calls; agent.Loop drops it once the
  // turn ends.
  Reasoning string `json:"reasoning_content,omitempty"`
  // Usage is what the server reported for the request that produced
  // this message. Never persisted.
  Usage Usage `json:"-"`
}

// Usage is the server's token accounting for one request.
type Usage struct {
  PromptTokens     int `json:"prompt_tokens"`
  CompletionTokens int `json:"completion_tokens"`
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
  Type        string        `json:"type"`
  Description string        `json:"description,omitempty"`
  Items       *ToolProperty `json:"items,omitempty"`
  Enum        []string      `json:"enum,omitempty"`
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

  // ID, if set, is the model name the server expects in requests —
  // a hosted API's id ("qwen/qwen3.8-27b"), where Model is armin's
  // alias for it.
  ID string

  // Effort, if set, caps how hard a reasoning model thinks: "none",
  // "minimal", "low", "medium", "high", "xhigh", "max". A hosted
  // model gets it as is; a local llama-server gets the template
  // switches and a thinking budget it maps to (see localEffort).
  Effort string

  // MaxTokens, if set, caps the reply (thinking included), so a model
  // that loops can't run on to the end of its window.
  MaxTokens int

  // Private, on a hosted API, asks OpenRouter to route only to
  // providers that don't collect prompts and keep nothing (zero data
  // retention). With no such provider for the model, the request
  // fails rather than going to one that logs.
  Private bool

  // APIKey, if set, is sent as "Authorization: Bearer <APIKey>" on
  // every request — for a remote OpenAI-compatible endpoint that
  // needs auth (Claude's, an OpenRouter/LiteLLM proxy, ...), unlike
  // a local llama-server, which needs none and leaves this empty.
  // internal/llamacpp.Manager resolves it from a models.<name>.
  // api_key_env config entry, never a literal key in config.conf.
  APIKey string

  // Fallback, if set, is the base URL a chat request moves to, once,
  // when BaseURL refuses it with 403 — OpenRouter's in-region host
  // without a Business plan. OnFallback hears the move, so the next
  // Client for the model starts on Fallback.
  Fallback   string
  OnFallback func(from, to string)

  // OnRequestStart/OnRequestEnd, if set, bracket every Chat/Complete
  // HTTP round trip (started before the request, ended via defer
  // once it returns). internal/llamacpp's Manager wires these to
  // know when a spawned model process is actually in use, so its
  // idle reaper never stops one mid-request. nil-safe: a zero-value
  // Client just skips them.
  OnRequestStart func()
  OnRequestEnd   func()

  // Ready, if set, runs before every request: internal/llamacpp uses
  // it to respawn a managed llama-server the idle reaper stopped.
  // It runs before OnRequestStart, so the in-flight count lands on
  // the process that actually serves the request.
  Ready func(ctx context.Context) error

  // ReadTimeout cancels a request once the server has sent nothing
  // for this long. Unlike a whole-request timeout it never kills a
  // long generation that keeps streaming. Zero uses
  // DefaultReadTimeout.
  ReadTimeout time.Duration
}

// OnToken is called once per content delta as the model streams a
// reply. It's nil-safe to skip: pass nil to Chat and it behaves
// like a plain non-streaming call.
type OnToken func(delta string)

// StreamHandlers receives a reply as it streams. Either field may be
// nil; with both nil the request isn't streamed at all.
type StreamHandlers struct {
  Token, Reasoning OnToken

  // Progress, if set, gets llama-server's prompt-processing
  // progress (tokens read of total) before the first token: on a
  // long context that's the silent stretch that looks like a hang.
  Progress func(done, total int)

  // Drafting, if set, hears a tool call's name as soon as the model
  // starts writing it, before its arguments are done.
  Drafting func(name string)
}

// idleWatch cancels a request's context once nothing has been read
// for the client's ReadTimeout.
type idleWatch struct {
  timer   *time.Timer
  after   time.Duration
  expired chan struct{}
}

type idleReader struct {
  r io.Reader
  w *idleWatch
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

  // Prompt is the text typed on the cursor's line so far. Sent apart
  // from the prefix (after the FIM middle token), it lets the server
  // reuse the cached prefix/suffix while you type along one line.
  Prompt string

  // Extra is repo context placed before the prefix: other files, or
  // the head of this one when it's outside the prefix window.
  Extra []InfillChunk

  // Indent is the cursor line's indentation: the server stops once a
  // generated line is indented less, i.e. at the end of the block.
  Indent int

  // MaxPredictMs ends generation at the first newline after this
  // many milliseconds, so a slow model still answers in time.
  MaxPredictMs int

  // TopK > 0 samples top-k/top-p then llama.cpp's infill sampler,
  // which prefers ending the fill over rambling past the suffix.
  TopK int
  TopP float64
}

// InfillChunk is one input_extra entry.
type InfillChunk struct {
  Filename string `json:"filename,omitempty"`
  Text     string `json:"text"`
}

type infillRequest struct {
  InputPrefix string        `json:"input_prefix"`
  InputSuffix string        `json:"input_suffix"`
  InputExtra  []InfillChunk `json:"input_extra,omitempty"`
  Prompt      string        `json:"prompt"` // required by /infill even when empty
  NPredict    int           `json:"n_predict,omitempty"`
  NIndent     int           `json:"n_indent,omitempty"`
  TMaxPredict int           `json:"t_max_predict_ms,omitempty"`
  Temperature float64       `json:"temperature"`
  TopK        int           `json:"top_k,omitempty"`
  TopP        float64       `json:"top_p,omitempty"`
  Samplers    []string      `json:"samplers,omitempty"`
  Stop        []string      `json:"stop,omitempty"`
  CachePrompt bool          `json:"cache_prompt"`
  Stream      bool          `json:"stream"`
}

type infillResponse struct {
  Content string `json:"content"`
}

type propsResponse struct {
  NCtx                      int `json:"n_ctx"`
  DefaultGenerationSettings struct {
    NCtx int `json:"n_ctx"`
  } `json:"default_generation_settings"`
}

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
  Role             string         `json:"role"`
  Content          string         `json:"content"`
  ReasoningContent string         `json:"reasoning_content,omitempty"`
  ToolCalls        []owaiToolCall `json:"tool_calls,omitempty"`
  ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type owaiStreamOptions struct {
  IncludeUsage bool `json:"include_usage"`
}

type owaiChatRequest struct {
  Model         string             `json:"model"`
  Messages      []owaiMessage      `json:"messages"`
  Tools         []owaiTool         `json:"tools,omitempty"`
  Stream        bool               `json:"stream"`
  StreamOptions *owaiStreamOptions `json:"stream_options,omitempty"`
  MaxTokens     int                `json:"max_tokens,omitempty"`

  // ReturnProgress is llama-server's: stream prompt_progress chunks.
  ReturnProgress bool `json:"return_progress,omitempty"`

  // Reasoning is OpenRouter's thinking budget, sent only when set.
  Reasoning *owaiReasoning `json:"reasoning,omitempty"`

  // Provider is OpenRouter's routing preferences: Client.Private.
  Provider *owaiProvider `json:"provider,omitempty"`

  // llama-server's: the chat template's own switches
  // (enable_thinking, reasoning_effort) and a hard cap on thinking
  // tokens, for models whose template has no effort levels.
  TemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
  ThinkBudget    *int           `json:"thinking_budget_tokens,omitempty"`
}

type owaiReasoning struct {
  Effort string `json:"effort"`
}

type owaiProvider struct {
  DataCollection string `json:"data_collection"` // "deny"
  ZDR            bool   `json:"zdr"`
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
  Usage   *Usage       `json:"usage,omitempty"`
  Error   *owaiError   `json:"error,omitempty"`
}

type owaiStreamToolCall struct {
  Index    int              `json:"index"`
  ID       string           `json:"id,omitempty"`
  Function owaiToolCallFunc `json:"function"`
}

type owaiStreamDelta struct {
  Content          string               `json:"content,omitempty"`
  ReasoningContent string               `json:"reasoning_content,omitempty"`
  ToolCalls        []owaiStreamToolCall `json:"tool_calls,omitempty"`
}

type owaiStreamChoice struct {
  Delta owaiStreamDelta `json:"delta"`
}

type owaiStreamChunk struct {
  Choices  []owaiStreamChoice `json:"choices"`
  Usage    *Usage             `json:"usage,omitempty"`
  Progress *promptProgress    `json:"prompt_progress,omitempty"`
}

type promptProgress struct {
  Total     int `json:"total"`
  Processed int `json:"processed"`
}
