package repl

import (
  "context"
  "errors"
  "fmt"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/llm"
  "armin/internal/share"
  "armin/internal/ui"
)

// stream modes for turnView.
const (
  modeIdle uint8 = iota
  modeThinking
  modeAnswer
)

func (v *turnView) stopSpinner() {
  if v.spinner != nil {
    v.spinner.Stop()
    v.spinner = nil
  }
}

func (v *turnView) startSpinner() {
  if v.spinner == nil {
    v.spinner = ui.NewStatus(v.status)
  }
}

const (
  phaseWaiting  = "waiting for the model"
  phaseReading  = "reading the prompt"
  phaseThinking = "thinking"

  // stallAfter is how long the model may stay silent before the
  // status says so.
  stallAfter = 20 * time.Second
)

// setPhase starts a phase; watch marks phases where silence means
// the model, not a tool, is slow.
func (v *turnView) setPhase(phase string, watch bool) {
  v.mu.Lock()
  defer v.mu.Unlock()

  now := time.Now()
  v.phase, v.since, v.last, v.watch, v.count = phase, now, now, watch, 0
}

// status is the spinner's text: what's happening, for how long, and
// whether the model has gone quiet.
func (v *turnView) status() string {
  v.mu.Lock()
  defer v.mu.Unlock()

  s := v.phase
  if v.count > 0 {
    s += fmt.Sprintf(" · %d tokens", v.count)
  }

  s += " · " + ui.Since(v.since)
  stalled := v.watch && time.Since(v.last) > stallAfter
  if stalled {
    s += " · nothing from the model for " + ui.Since(v.last)
  } else if v.phase == phaseWaiting && time.Since(v.since) > 5 * time.Second {
    s += " (it may be loading or busy)"
  }

  // A tip only where it fits: the spinner line must not wrap.
  tip := tipAt(v.tips, v.tipBase, time.Since(v.start))
  if tip != "" {
    w := ui.TextWidth()
    if w <= 0 || len([]rune(s)) + len([]rune(tip)) + 10 <= w {
      s += " · tip: " + tip
    }
  }

  return s
}

func (v *turnView) progress(done, total int) {
  v.mu.Lock()
  defer v.mu.Unlock()

  if !strings.HasPrefix(v.phase, phaseReading) {
    v.since = time.Now()
  }

  v.phase = fmt.Sprintf("%s %s/%s", phaseReading, kTokens(done), kTokens(total))
  v.last = time.Now()
}

// endLine closes whatever was streaming, so the next thing printed
// starts on its own line.
func (v *turnView) endLine() {
  v.flush()
  if v.mode != modeIdle {
    fmt.Println()
  }

  v.wrap.Reset()
  v.markdown().Reset()
  v.mode = modeIdle
  v.pending = 0
}

// flush prints the word the wrapper holds back, styled like the
// stream it belongs to.
func (v *turnView) flush() {
  if v.mode == modeAnswer {
    v.markdown().Flush()
    return
  }

  rest := v.wrap.Flush()
  if rest == "" {
    return
  }

  if v.mode == modeThinking {
    rest = ui.Dim(rest)
  }

  fmt.Print(rest)
}

func (v *turnView) reasoning(delta string) {
  if !v.showThinking {
    v.mu.Lock()
    if v.phase != phaseThinking {
      v.phase, v.since, v.count = phaseThinking, time.Now(), 0
    }

    v.count++
    v.last = time.Now()
    v.mu.Unlock()

    return
  }

  v.stopSpinner()
  if v.mode != modeThinking {
    v.endLine()
    v.mode = modeThinking
  }

  out := v.wrap.Write(delta)
  if out != "" {
    fmt.Print(ui.Dim(out))
  }
}

func (v *turnView) token(delta string) {
  // Whitespace before an answer starts would leave the cursor mid
  // line, and the next tool line would print from there.
  if v.mode != modeAnswer {
    delta = strings.TrimLeft(delta, " \t\n")
    if delta == "" {
      return
    }
  }

  v.stopSpinner()
  if v.mode == modeThinking {
    v.flush()
    fmt.Print("\n\n")
    v.wrap.Reset()
  }

  v.mode = modeAnswer
  v.streamed = true
  v.answer(delta)
}

// maxBreak is the most newlines in a row an answer prints: one blank
// line between paragraphs, however many the model sent.
const maxBreak = 2

// answer prints streamed answer text. Newlines it ends on are held
// back: a tool call next drops them (the ● line already starts a
// new line), more text gets at most one blank line before it.
func (v *turnView) answer(delta string) {
  body := strings.TrimLeft(delta, "\n")
  v.pending += len(delta) - len(body)
  if body == "" {
    return
  }

  // A trailing run of whitespace holding a newline waits too; one
  // without (a space between two words) prints as is.
  trimmed := strings.TrimRight(body, " \t\n")
  held := strings.Count(body[len(trimmed):], "\n")
  if held > 0 {
    body = trimmed
  }

  if body == "" {
    v.pending += held
    return
  }

  if v.pending > 0 {
    v.markdown().Write(strings.Repeat("\n", min(v.pending, maxBreak)))
  }

  for strings.Contains(body, "\n\n\n") {
    body = strings.ReplaceAll(body, "\n\n\n", "\n\n")
  }

  v.markdown().Write(body)
  v.pending = held
}

// markdown is the answer's renderer, made on first use.
func (v *turnView) markdown() *ui.Markdown {
  if v.md == nil {
    v.md = ui.NewMarkdown()
  }

  return v.md
}

// proposed are the tools whose call shows as a proposal block
// (● Update, ● Create, ● Run) instead of a call line.
var proposed = map[string]bool{
  "edit_file": true, "write_file": true, "run_program": true,
}

func (v *turnView) toolCall(name string, args map[string]any) {
  v.stopSpinner()
  v.endLine()
  ui.ClearLine()

  // Held until the proposal shows; printed only if none does (the
  // call failed before it could be proposed).
  v.held = ""
  if proposed[name] {
    v.held = agent.CallLine(name, args)
  } else {
    ui.ToolLine(agent.CallLine(name, args))
  }

  what := name
  bin, _ := args["binary"].(string)
  if bin != "" {
    what = bin
  }

  v.setPhase("running " + what, false)
  v.startSpinner()
}

func (v *turnView) toolResult(name, result string) {
  v.toolSummary(agent.ResultLine(name, result))
}

// toolSummary closes a tool call with its one-line result.
func (v *turnView) toolSummary(line string) {
  v.stopSpinner()
  ui.ClearLine()
  if v.held != "" {
    ui.ToolLine(v.held)
    v.held = ""
  }

  ui.ToolResultLine(line)
  v.setPhase(phaseWaiting, true)
  v.startSpinner()
}

// turn runs one user message through the agent loop. Ctrl-C (see
// Interrupt) cancels just this turn.
func (r *REPL) turn(ctx context.Context, input string) error {
  return r.turnAs(ctx, input, input, originTerminal)
}

// turnAs is turn for whoever asked: shown is the question as the
// other frontends display it, input what goes in the history.
func (r *REPL) turnAs(ctx context.Context, input, shown, origin string) error {
  r.busy.Store(true)
  defer r.busy.Store(false)
  defer r.snapshot()

  r.emit(share.Event{ Type: "user", Delta: shown, Origin: origin })

  r.lastSuggestions = nil
  r.History = append(
    r.History, llm.Message{ Role: "user", Content: input },
  )

  turnCtx, cancel := context.WithCancel(ctx)
  r.turnMu.Lock()
  r.cancelTurn = cancel
  r.turnMu.Unlock()
  defer func() {
    r.turnMu.Lock()
    r.cancelTurn = nil
    r.turnMu.Unlock()
    cancel()
  }()

  v := &turnView{
    showThinking: r.state & flagHideThinking == 0,
    wrap:         ui.NewWrapper(ui.TextWidth()),
    tips:         replTips,
    tipBase:      tipTurn.Add(1),
    start:        time.Now(),
  }

  v.setPhase(phaseWaiting, true)
  v.startSpinner()

  hooks := agent.Hooks{
    Token:      v.token,
    Reasoning:  v.reasoning,
    ToolCall:   v.toolCall,
    ToolResult: v.toolResult,
    Progress:   v.progress,
    Usage:      func(u llm.Usage) { r.lastTokens = u.Total() },
    Notice: func(msg string) {
      v.stopSpinner()
      v.endLine()
      ui.Info("context: %s", msg)
      v.startSpinner()
    },
    Compacted: func(before, after int) {
      v.stopSpinner()
      v.endLine()
      ui.Info(
        "context: compacted ~%dk → ~%dk tokens", before / 1000, after / 1000,
      )
      r.persisted = 0  // history was rewritten; next save is full
      r.lastTokens = 0 // the gauge shows an estimate until the next call
      v.startSpinner()
    },
  }

  {
    // Every write and run is shown before it happens, asked about or
    // not (auto-approve only skips the question).
    hooks.Propose = func(p agent.Proposal) {
      v.stopSpinner()
      v.endLine()
      ui.ClearLine()
      v.held = "" // the proposal block is the call's header
      r.showProposal(p)
      r.proposal = &p
      v.setPhase(phaseWaiting, false)
      v.startSpinner()
    }

    hooks.Approve = func(tool, preview string) (bool, string) {
      // Auto-approve covers file writes and runs; access outside the
      // project and privileged programs are always asked.
      if r.state & flagAutoApprove != 0 && !agent.AlwaysAsk(tool) {
        return true, ""
      }

      v.stopSpinner()
      v.endLine()
      ok, feedback := r.approve(tool, preview)
      v.startSpinner()

      return ok, feedback
    }
  }

  // The input bar: what's typed meanwhile goes to the model at its
  // next step (hooks.Steer); questions pause it.
  hooks.Steer = func() []string {
    msgs := r.takeQueued()
    if len(msgs) == 0 {
      return nil
    }

    v.stopSpinner()
    v.endLine()
    for _, m := range msgs {
      fmt.Println(ui.Prompt() + m)
      r.emit(share.Event{ Type: "user", Delta: m, Origin: origin })
    }

    r.composer.Taken(len(msgs))
    v.setPhase(phaseWaiting, true)
    v.startSpinner()

    return msgs
  }

  approve := hooks.Approve
  hooks.Approve = func(tool, preview string) (bool, string) {
    r.composer.Pause()
    defer r.composer.Resume()

    return approve(tool, preview)
  }

  r.composer = ui.StartComposer(r.pump, "type to add a message — the model reads it at its next step", r.enqueue)
  answer, err := r.Loop.Turn(turnCtx, &r.History, r.shared(hooks, origin))
  r.endComposer()

  v.stopSpinner()
  if errors.Is(err, context.Canceled) && ctx.Err() == nil {
    v.endLine()
    ui.Info("interrupted")
    r.emit(share.Event{ Type: "error", Delta: "stopped", Origin: origin })

    return nil
  }

  if err != nil {
    v.endLine()
    r.emit(share.Event{ Type: "error", Delta: err.Error(), Origin: origin })

    return err
  }

  r.emit(share.Event{ Type: "done", Delta: answer, Origin: origin })

  if v.streamed {
    v.flush()
    fmt.Println()
  } else if answer != "" {
    v.endLine()
    fmt.Println(ui.RenderMarkdown(answer, ui.TextWidth()))
  }

  r.maybeShowSuggestions(ctx)

  return nil
}

// approve shows a write's diff and asks. "a" approves this and every
// later write this session; any other text declines and goes back to
// the model as feedback.
func (r *REPL) approve(tool, preview string) (bool, string) {
  // A write or run was just shown in full (showProposal); anything
  // else (read access outside the project) shows its preview here.
  shown := r.proposal
  r.proposal = nil
  if shown == nil {
    for _, line := range strings.Split(strings.TrimSuffix(preview, "\n"), "\n") {
      switch {
      case strings.HasPrefix(line, "+ "):
        fmt.Println("  " + ui.Green(line))
      case strings.HasPrefix(line, "- "):
        fmt.Println("  " + ui.Red(line))
      case strings.HasPrefix(line, "  "):
        fmt.Println("  " + ui.Dim(line))
      default:
        fmt.Println("  " + ui.Bold(line))
      }
    }
  }

  question := "apply " + tool + "?"
  switch tool {
  case "request_access":
    question = "grant read access?"
  case "run_program", agent.ApprovePrivileged:
    question = "run it?"
  }

  // What's always asked can't be approved for the whole session.
  keys, legend := "y/n", "y yes · n no"
  if !agent.AlwaysAsk(tool) {
    keys, legend = keys + "/a", legend + " · a all this session"
  }

  if shown != nil {
    keys, legend = keys + "/v", legend + " · v view in nvim"
  }

  // The legend once, the question short: a long question wraps in a
  // narrow terminal.
  fmt.Println(ui.Dim("  " + legend + " · anything else is feedback to the model"))

  for {
    answer, err := r.ask(ui.Yellow(question) + ui.Dim(" [" + keys + "] "))
    if err != nil {
      return false, ""
    }

    answer = strings.TrimSpace(answer)
    switch strings.ToLower(answer) {
    case "y", "yes", "":
      return true, ""
    case "a", "all":
      if agent.AlwaysAsk(tool) {
        return true, "" // just this once
      }

      r.state |= flagAutoApprove

      return true, ""
    case "n", "no":
      return false, ""
    case "v", "view":
      if shown == nil {
        return false, answer
      }

      err := r.view(*shown)
      if err != nil {
        ui.Error("can't open it: %s", err)
      } else {
        ui.Info("opened in a new window (read-only) — answer here when you're done")
      }

      continue
    }

    return false, answer
  }
}

// ask reads one answer to a question. Once Run started, the key pump
// owns stdin, so questions read through it too: reading the terminal
// beside it would split the keys between the two.
func (r *REPL) ask(prompt string) (string, error) {
  if r.pump == nil {
    fmt.Print(prompt)
    return r.reader.ReadString('\n')
  }

  answer, err := ui.ReadLineWake(r.runCtx, r.pump, prompt, "", nil, nil)
  if errors.Is(err, ui.ErrInterrupted) {
    return "n", nil // Ctrl-C at a question declines
  }

  return answer, err
}
