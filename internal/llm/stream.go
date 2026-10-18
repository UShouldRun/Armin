// Reading a reply: readStreamedReply assembles server-sent events
// (content, thinking, tool-call fragments by index, usage) into one
// Message, readWholeReply decodes a non-streamed one; watchIdle and
// idleReader time out a server that goes silent, not one that's slow.

package llm

import (
  "bufio"
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "sort"
  "strings"
  "time"
)

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
  body, err := io.ReadAll(io.LimitReader(r, maxReply))
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
      "\n\t... in readWholeReply:\n\t parsing llama-server response: %w " +
        "(body: %s)",
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
      "\n\t... in readWholeReply:\n\t no choices in llama-server response " +
        "(body: %s)",
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
        if acc.name == "" && h.Drafting != nil {
          h.Drafting(tc.Function.Name)
        }

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
          "\n\t... in readStreamedReply:\n\t parsing streamed tool call " +
            "arguments: %w (raw: %s)",
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
