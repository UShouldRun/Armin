// One turn in the terminal: turnAs runs it (see its flow comment),
// turnHooks wires the agent loop's callbacks to the screen, approve
// asks before a write or a run, ask reads one answer.

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

// proposed are the tools whose call shows as a proposal block
// (● Update, ● Create, ● Run) instead of a call line.
var proposed = map[string]bool{
  "edit_file": true, "write_file": true, "run_program": true,
}

// turn runs one user message through the agent loop. Ctrl-C (see
// Interrupt) cancels just this turn.
func (r *REPL) turn(ctx context.Context, input string) error {
  return r.turnAs(ctx, input, input, originTerminal)
}

// turnAs is turn for whoever asked: shown is the question as the
// other frontends display it, input what goes in the history.
//
// The flow: the question goes to every frontend (emit) and into the
// history; the turn gets its own context, so Ctrl-C (Interrupt)
// stops just this turn; the effort and kind of task are settled
// (turnPlan); then the agent loop runs with hooks that draw it in the
// terminal (turnHooks, turnView) while the composer below takes what
// the user types meanwhile. The answer is printed, rendered as
// Markdown, and sent to joined frontends.
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

  effort, task := r.turnPlan(turnCtx, input)

  v.setPhase(phaseWaiting, true)
  v.startSpinner()

  hooks := r.turnHooks(v, effort, task, origin)

  r.composer = ui.StartComposer(r.pump,
    "type to add a message — the model reads it at its next step", r.enqueue)
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

// turnHooks draws a turn in the terminal: tokens and thinking stream
// through v, tool calls get a line each, writes and runs show in full
// before they happen (Propose) and wait for a yes (Approve, which
// pauses the composer), and what's typed meanwhile reaches the model
// at its next step (Steer).
func (r *REPL) turnHooks(
  v *turnView, effort agent.Effort, task agent.Task, origin string,
) agent.Hooks {
  hooks := agent.Hooks{
    Effort:     effort.Wire(),
    Task:       task,
    Aside: func(msg string) {
      v.stopSpinner()
      v.endLine()
      ui.Info("%s", msg)
    },
    Token:      v.token,
    Reasoning:  v.reasoning,
    ToolCall:   v.toolCall,
    ToolResult: v.toolResult,
    Progress:   v.progress,
    Drafting:   v.drafting,
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
      v.startSpinner() // still the tool's phase: it runs next

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
      v.resetClock()
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

  return hooks
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
