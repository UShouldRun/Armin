// This REPL as the project's instance: the share.Host methods joined
// frontends call, run on the REPL's goroutine through an inbox so the
// conversation has one owner.

package repl

import (
  "context"
  "errors"
  "fmt"
  "sort"
  "strings"

  "armin/internal/agent"
  "armin/internal/llm"
  "armin/internal/share"
  "armin/internal/ui"
)

// originTerminal names this terminal's questions to the others.
const originTerminal = "terminal"

// The REPL is a share.Host, through Host, when it is the project's
// instance.
var _ share.Host = host{}

// Host is what share.Listen serves.
func (r *REPL) Host() share.Host { return host{ r } }

// Share makes this REPL the project's instance (see share.Listen).
func (r *REPL) Share(hub *share.Hub) { r.hub = hub }

func (r *REPL) emit(e share.Event) { r.hub.Emit(e) }

// post runs f on the REPL goroutine at the next prompt, waking it.
func (r *REPL) post(f func()) {
  r.inbox <- f
  select {
  case r.wake <- struct{}{}:
  default:
  }
}

// runInbox runs what other frontends posted, on the REPL goroutine.
func (r *REPL) runInbox() {
  for {
    select {
    case f := <-r.inbox:
      f()
    default:
      return
    }
  }
}

// snapshot refreshes History for the other frontends; REPL
// goroutine only.
func (r *REPL) snapshot() {
  lines := share.Lines(r.History)
  r.lines.Store(&lines)
}

// shared wraps a turn's hooks so every frontend sees the turn too.
func (r *REPL) shared(h agent.Hooks, origin string) agent.Hooks {
  if r.hub == nil {
    return h
  }

  send := func(e share.Event) {
    e.Origin = origin
    r.hub.Emit(e)
  }

  thinking := false
  out := h
  out.Token = func(delta string) {
    thinking = false
    send(share.Event{ Type: "token", Delta: delta })
    if h.Token != nil {
      h.Token(delta)
    }
  }

  out.Reasoning = func(delta string) {
    if !thinking {
      thinking = true
      send(share.Event{ Type: "reasoning" })
    }

    if h.Reasoning != nil {
      h.Reasoning(delta)
    }
  }

  out.ToolCall = func(name string, args map[string]any) {
    thinking = false
    send(share.Event{ Type: "tool_call", Name: name, Args: args })
    if h.ToolCall != nil {
      h.ToolCall(name, args)
    }
  }

  out.ToolResult = func(name, result string) {
    send(share.Event{ Type: "tool_result", Name: name, Delta: agent.ResultLine(name, result) })
    if h.ToolResult != nil {
      h.ToolResult(name, result)
    }
  }

  out.Notice = func(msg string) {
    send(share.Event{ Type: "notice", Delta: msg })
    if h.Notice != nil {
      h.Notice(msg)
    }
  }

  out.Usage = func(u llm.Usage) {
    send(share.Event{
      Type: "usage", Tokens: u.Total(),
      Window: r.Loop.WindowSize(), Tier: r.Loop.Tier(),
    })
    if h.Usage != nil {
      h.Usage(u)
    }
  }

  return out
}

// Ask queues another frontend's question for the next prompt, where
// it runs like a typed one: approvals are asked here.
func (h host) Ask(_ context.Context, question, file, origin string) error {
  r := h.r

  if !r.busy.CompareAndSwap(false, true) {
    return share.ErrBusy
  }

  r.post(func() {
    fmt.Println()
    fmt.Println(ui.Dim("  " + origin + " asked:"))
    fmt.Println(ui.Prompt() + question)

    err := r.turnAs(r.runCtx, share.Question(question, file), question, origin)
    if err != nil {
      ui.Error("%s", err)
    }

    r.maybeAutosave()
  })

  return nil
}

func (h host) Stop() { h.r.Interrupt() }

func (h host) Clear(origin string) error {
  r := h.r

  if r.busy.Load() {
    return errors.New("still answering — stop it first")
  }

  r.post(func() {
    r.History = r.History[:1]
    r.lastTokens = 0
    resyncSession(r)
    r.snapshot()
    ui.Info("conversation cleared from %s", origin)
    r.emit(share.Event{ Type: "cleared", Origin: origin })
  })

  return nil
}

func (h host) History() []share.ChatLine {
  r := h.r

  lines := r.lines.Load()
  if lines == nil {
    return []share.ChatLine{}
  }

  return *lines
}

func (h host) Models() share.ModelInfo {
  r := h.r

  current := *r.current.Load()
  info := share.ModelInfo{
    Current: current, Completion: *r.completing.Load(), Warning: r.Models.Warning,
  }
  if r.hub != nil {
    info.Instance = r.hub.Instance
  }

  for _, s := range r.Models.Names() {
    info.Configured = append(info.Configured, share.ModelChoice{
      Name: s.Name, Model: s.Model, Managed: s.Managed, Running: s.Running,
    })
  }

  sort.Slice(info.Configured, func(i, j int) bool {
    return info.Configured[i].Name < info.Configured[j].Name
  })

  hosted := r.Models.OpenRouter()
  url, key := r.Models.Endpoint(hosted)
  share.Suggest(&info, hosted, url, key)

  return info
}

// SwitchModel switches on the REPL goroutine, between turns.
func (h host) SwitchModel(ctx context.Context, name string) (share.ModelInfo, error) {
  r := h.r

  if r.busy.Load() {
    return share.ModelInfo{}, errors.New("still answering — stop it first")
  }

  if !r.Models.Has(name) {
    return share.ModelInfo{}, fmt.Errorf("no model %q in config.conf", name)
  }

  done := make(chan struct{})
  r.post(func() {
    r.switchModel(r.runCtx, name)
    close(done)
  })

  select {
  case <-done:
  case <-ctx.Done():
    return share.ModelInfo{}, ctx.Err()
  }

  info := h.Models()
  if info.Current != name {
    return info, fmt.Errorf("couldn't switch to %q", name)
  }

  return info, nil
}

// Complete is inline completion with the chat model: the GPU holds
// one model.
func (h host) Complete(ctx context.Context, f share.Fill) (string, error) {
  r := h.r

  return r.completer().Complete(ctx, f.Prefix, f.Suffix, f.Opts)
}

// Explain answers a one-off question (nvim's hover) outside the
// conversation, with read-only tools.
func (h host) Explain(ctx context.Context, question string) (string, error) {
  r := h.r

  if r.explain == nil {
    l := agent.New(r.LLM, r.Sandbox)
    l.WebSearch, l.Symbols = r.Loop.WebSearch, r.Loop.Symbols
    l.Memories, l.Skills, l.Scratch = r.Loop.Memories, r.Loop.Skills, r.Loop.Scratch
    l.MaxRounds = r.Loop.MaxRounds
    l.ReadOnly = true
    r.explain = l
  }

  history := []llm.Message{
    { Role: "system", Content: r.system },
    { Role: "user", Content: question },
  }

  return r.explain.Turn(ctx, &history, agent.Hooks{})
}

// completer is the Client for inline completion: Completion, or chat
// itself when none was given.
func (r *REPL) completer() *llm.Client {
  if r.Completion != nil {
    return r.Completion
  }

  return r.LLM
}

// SetCompleting names the completion model for the other frontends.
func (r *REPL) SetCompleting(name string) { r.completing.Store(&name) }

// enqueue keeps a message typed during a turn for its next step.
func (r *REPL) enqueue(msg string) {
  r.queueMu.Lock()
  r.queued = append(r.queued, msg)
  r.queueMu.Unlock()
}

func (r *REPL) takeQueued() []string {
  r.queueMu.Lock()
  defer r.queueMu.Unlock()

  msgs := r.queued
  r.queued = nil

  return msgs
}

// endComposer removes the input bar after a turn. Messages the turn
// never took (it stopped, or failed) and an unsent line come back in
// the next prompt, to send or change.
func (r *REPL) endComposer() {
  leftover := r.composer.Stop()
  r.composer = nil

  parts := r.takeQueued()
  if leftover != "" {
    parts = append(parts, leftover)
  }

  r.draft = strings.Join(parts, " ")
}
