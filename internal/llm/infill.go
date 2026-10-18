// Beyond chat: Complete for inline code completion (llama-server's
// /infill, fill-in-the-middle), Health and ContextSize to ask a
// server whether it's up and how large its window is.

package llm

import (
  "bytes"
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
)

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

  respBody, err := io.ReadAll(
    io.LimitReader(idleReader{ r: resp.Body, w: idle }, maxReply),
  )
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
    body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
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
