// Package llm wraps Ollama's /api/chat, /api/generate, and
// /api/tags endpoints: chat with native tool calling and token
// streaming for the agent loop, and a separate raw completion call
// for latency-sensitive inline code completion — no MCP involved in
// this client, see plan section 1.
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
  "strings"
  "time"
)

// ErrToolsUnsupported means the configured model rejected the
// request because it has no tool-calling support at all (Ollama's
// own wording is a raw "<model> does not support tools" 400) — no
// prompt or retry fixes this, only switching to a different model
// does. Callers match it with errors.Is to show a clean, actionable
// message instead of Ollama's HTTP body.
var ErrToolsUnsupported = errors.New("model does not support tool calling")

// Message mirrors Ollama's chat message shape. Role is one of
// "system", "user", "assistant", or "tool".
type Message struct {
  Role      string     `json:"role"`
  Content   string     `json:"content"`
  ToolCalls []ToolCall `json:"tool_calls,omitempty"`
  // ToolName is set on role:"tool" messages so the model can match
  // the result back to the call that produced it.
  ToolName string `json:"tool_name,omitempty"`
}

// ToolCall is a single tool invocation the model asked for.
type ToolCall struct {
  Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
  Name      string         `json:"name"`
  Arguments map[string]any `json:"arguments"`
}

// ToolDefinition describes one callable tool using the same
// JSON-schema shape Ollama (and OpenAI-style APIs) expect. Keep
// Description short — small models are sensitive to prompt bloat,
// and tool schemas share the same context budget as the
// conversation.
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

type chatRequest struct {
  Model    string           `json:"model"`
  Messages []Message        `json:"messages"`
  Tools    []ToolDefinition `json:"tools,omitempty"`
  Stream   bool             `json:"stream"`
}

type chatChunk struct {
  Message Message `json:"message"`
  Done    bool    `json:"done"`
}

// Client is a minimal Ollama HTTP client.
type Client struct {
  BaseURL string
  Model   string
  HTTP    *http.Client
}

// New returns a Client pointed at a local Ollama instance.
func New(baseURL, model string) *Client {
  if baseURL == "" {
    baseURL = "http://localhost:11434"
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

// Chat sends the full message history plus tool definitions and
// returns the model's next message (which may itself contain tool
// calls). Ollama has no server-side conversation state, so callers
// must resend the whole history every turn. When onToken is
// non-nil, content deltas are streamed to it as they arrive — tool
// calls, if any, only ever show up complete on the final chunk, so
// there's nothing to stream for those.
func (c *Client) Chat(
  ctx context.Context, messages []Message,
  tools []ToolDefinition, onToken OnToken,
) (*Message, error) {
  reqBody := chatRequest{
    Model:    c.Model,
    Messages: messages,
    Tools:    tools,
    Stream:   onToken != nil,
  }

  body, err := json.Marshal(reqBody)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Chat:\n\t marshaling chat request: %w", err,
    )
  }

  url := c.BaseURL + "/api/chat"
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
      "\n\t... in Chat:\n\t calling ollama at %s: %w", c.BaseURL, err,
    )
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    respBody, _ := io.ReadAll(resp.Body)
    if strings.Contains(string(respBody), "does not support tools") {
      return nil, fmt.Errorf(
        "\n\t... in Chat:\n\t %w: %s", ErrToolsUnsupported, c.Model,
      )
    }
    return nil, fmt.Errorf(
      "\n\t... in Chat:\n\t ollama returned %d: %s",
      resp.StatusCode, string(respBody),
    )
  }

  if onToken == nil {
    return readWholeReply(resp.Body)
  }
  return readStreamedReply(resp.Body, onToken)
}

func readWholeReply(r io.Reader) (*Message, error) {
  body, err := io.ReadAll(r)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readWholeReply:\n\t reading ollama response: %w", err,
    )
  }

  var chunk chatChunk
  if err := json.Unmarshal(body, &chunk); err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readWholeReply:\n\t parsing ollama response: %w (body: %s)",
      err, string(body),
    )
  }

  return &chunk.Message, nil
}

// readStreamedReply consumes Ollama's newline-delimited JSON
// stream, forwarding each content delta to onToken as it arrives,
// and returns the fully assembled message once the final
// ("done":true) chunk lands.
func readStreamedReply(r io.Reader, onToken OnToken) (*Message, error) {
  scanner := bufio.NewScanner(r)
  scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

  final := Message{ Role: "assistant" }
  var content bytes.Buffer

  for scanner.Scan() {
    line := scanner.Bytes()
    if len(line) == 0 {
      continue
    }

    var chunk chatChunk
    if err := json.Unmarshal(line, &chunk); err != nil {
      return nil, fmt.Errorf(
        "\n\t... in readStreamedReply:\n\t parsing chunk: %w (line: %s)",
        err, string(line),
      )
    }

    if chunk.Message.Content != "" {
      content.WriteString(chunk.Message.Content)
      onToken(chunk.Message.Content)
    }
    if len(chunk.Message.ToolCalls) > 0 {
      final.ToolCalls = chunk.Message.ToolCalls
    }

    if chunk.Done {
      break
    }
  }

  if err := scanner.Err(); err != nil {
    return nil, fmt.Errorf(
      "\n\t... in readStreamedReply:\n\t streaming ollama response: %w",
      err,
    )
  }

  final.Content = content.String()
  return &final, nil
}

// CompleteOptions tunes one Complete call. Model overrides c.Model
// — inline completion wants a small fill-in-the-middle code model,
// not whatever chat model the agent loop is using.
type CompleteOptions struct {
  Model       string
  Temperature float64
  NumPredict  int // cap on generated tokens — this is a completion, not an essay
  Stop        []string
}

type generateRequest struct {
  Model   string         `json:"model"`
  Prompt  string         `json:"prompt"`
  Suffix  string         `json:"suffix,omitempty"`
  Stream  bool           `json:"stream"`
  Options map[string]any `json:"options,omitempty"`
}

type generateResponse struct {
  Response string `json:"response"`
}

// Complete asks a fill-in-the-middle-capable model to continue
// prefix with suffix (the code already after the cursor) in view,
// via Ollama's /api/generate — no chat formatting, no tool calling,
// just raw code continuation. This is the latency-sensitive
// inline-completion path; it never touches the agent loop.
func (c *Client) Complete(
  ctx context.Context, prefix, suffix string, opts CompleteOptions,
) (string, error) {
  model := opts.Model
  if model == "" {
    model = c.Model
  }

  options := map[string]any{ "temperature": opts.Temperature }
  if opts.NumPredict > 0 {
    options["num_predict"] = opts.NumPredict
  }
  if len(opts.Stop) > 0 {
    options["stop"] = opts.Stop
  }

  body, err := json.Marshal(generateRequest{
    Model:   model,
    Prompt:  prefix,
    Suffix:  suffix,
    Stream:  false,
    Options: options,
  })
  if err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }

  url := c.BaseURL + "/api/generate"
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
      "\n\t... in Complete:\n\t calling ollama at %s: %w",
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
      "\n\t... in Complete:\n\t ollama returned %d: %s",
      resp.StatusCode, string(respBody),
    )
  }

  var parsed generateResponse
  if err := json.Unmarshal(respBody, &parsed); err != nil {
    return "", fmt.Errorf("\n\t... in Complete:\n\t%w", err)
  }
  return parsed.Response, nil
}

// TagsModel is one entry from GET /api/tags — the models Ollama has
// pulled locally. Used by the /model command to offer real choices
// instead of an unchecked free-text name.
type TagsModel struct {
  Name string `json:"name"`
  Size int64  `json:"size"` // bytes on disk — a rough proxy for parameter count
}

type tagsResponse struct {
  Models []TagsModel `json:"models"`
}

// ListModels returns the models available on the local Ollama
// daemon.
func (c *Client) ListModels(ctx context.Context) ([]TagsModel, error) {
  url := c.BaseURL + "/api/tags"
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ListModels:\n\t%w", err)
  }

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ListModels:\n\t calling ollama at %s: %w",
      c.BaseURL, err,
    )
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    body, _ := io.ReadAll(resp.Body)
    return nil, fmt.Errorf(
      "\n\t... in ListModels:\n\t ollama returned %d: %s",
      resp.StatusCode, string(body),
    )
  }

  var parsed tagsResponse
  if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
    return nil, fmt.Errorf("\n\t... in ListModels:\n\t%w", err)
  }

  return parsed.Models, nil
}
