// Starting armin fast: the chat model, when armin runs it on this
// machine, loads in the background, so the prompt shows at once and a
// first message waits for the model only if it's sent before the load
// ends; the frontend says when the model is ready and how long the
// load took. Each step's time also goes into the metrics
// (startup.<step>.ms) and Mind.Startup; ARMIN_STARTUP=1 prints them.

package think

import (
  "context"
  "fmt"
  "io"
  "time"

  "armin/internal/llm"
  "armin/internal/metrics"
)

// boot is when the process started: package variables are set before
// main runs.
var boot = time.Now()

// took records a step that began at start.
func (m *Mind) took(name string, start time.Time) {
  d := time.Since(start)
  m.Startup = append(m.Startup, Stage{ Name: name, Took: d })
  metrics.Observe("startup." + name + ".ms", float64(d.Microseconds()) / 1000)
}

// SinceBoot is the time since the process started.
func SinceBoot() time.Duration {
  return time.Since(boot)
}

// PrintStartup writes every step and the total.
func (m *Mind) PrintStartup(w io.Writer) {
  for _, s := range m.Startup {
    ms := float64(s.Took.Microseconds()) / 1000
    fmt.Fprintf(w, "  %-22s %8.1f ms\n", s.Name, ms)
  }

  fmt.Fprintf(
    w, "  %-22s %8.1f ms\n", "total since start",
    float64(SinceBoot().Microseconds()) / 1000,
  )
}

// chatClient is the client for the chat model. One armin runs on this
// machine loads in the background (its first request waits for it);
// any other (hosted, or a server already running) is ready at once.
func (m *Mind) chatClient(
  ctx context.Context, name string,
) (*llm.Client, error) {
  if !m.Models.IsLocal(name) {
    t0 := time.Now()
    client, err := m.Models.Client(ctx, name)
    m.took("models.chat-ready", t0)

    return client, err
  }

  client, err := m.Models.LazyClient(name)
  if err != nil {
    return nil, err
  }

  go func() {
    t0 := time.Now()
    err := client.Ready(ctx)
    took := time.Since(t0)
    metrics.Observe(
      "startup.models.chat-load.ms",
      float64(took.Microseconds()) / 1000,
    )
    switch {
    case err != nil && ctx.Err() == nil:
      m.notice(fmt.Sprintf("model %s failed to load: %s", name, err))
    case err == nil:
      m.notice(
        fmt.Sprintf("model ready: %s loaded in %.1fs", name, took.Seconds()),
      )
    }
  }()

  return client, nil
}

// notice queues a message for the frontend, dropping it when the queue
// is full rather than blocking the work that sent it.
func (m *Mind) notice(msg string) {
  select {
  case m.Notices <- msg:
  default:
  }
}

