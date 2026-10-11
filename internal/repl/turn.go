package repl

import (
  "context"
  "errors"
  "fmt"
  "strings"

  "armin/internal/agent"
  "armin/internal/llm"
  "armin/internal/ui"
)

// stream modes for turnView.
const (
  modeIdle uint8 = iota
  modeThinking
  modeAnswer
)

// turnView owns the terminal during one turn: the spinner, streamed
// thinking (dim) and answer text, tool-call lines and approvals.
type turnView struct {
  spinner      *ui.Spinner
  mode         uint8
  showThinking bool
  streamed     bool
}

func (v *turnView) stopSpinner() {
  if v.spinner != nil {
    v.spinner.Stop()
    v.spinner = nil
  }
}

func (v *turnView) startSpinner() {
  if v.spinner == nil {
    v.spinner = ui.NewSpinner("thinking")
  }
}

// endLine closes whatever was streaming, so the next thing printed
// starts on its own line.
func (v *turnView) endLine() {
  if v.mode != modeIdle {
    fmt.Println()
  }

  v.mode = modeIdle
}

func (v *turnView) reasoning(delta string) {
  if !v.showThinking {
    return // the spinner keeps saying "thinking"
  }

  v.stopSpinner()
  if v.mode != modeThinking {
    v.endLine()
    v.mode = modeThinking
  }

  fmt.Print(ui.Dim(delta))
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
    fmt.Print("\n\n")
  }

  v.mode = modeAnswer
  v.streamed = true
  fmt.Print(delta)
}

func (v *turnView) toolCall(name string, args map[string]any) {
  v.stopSpinner()
  v.endLine()
  ui.ClearLine()
  ui.ToolLine(agent.CallLine(name, args))
  v.startSpinner()
}

func (v *turnView) toolResult(name, result string) {
  v.stopSpinner()
  ui.ClearLine()
  ui.ToolResultLine(agent.ResultLine(name, result))
  v.startSpinner()
}

// turn runs one user message through the agent loop. Ctrl-C (see
// Interrupt) cancels just this turn.
func (r *REPL) turn(ctx context.Context, input string) error {
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

  v := &turnView{ showThinking: r.state & flagHideThinking == 0 }
  v.startSpinner()

  hooks := agent.Hooks{
    Token:      v.token,
    Reasoning:  v.reasoning,
    ToolCall:   v.toolCall,
    ToolResult: v.toolResult,
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

  if r.state & flagAutoApprove == 0 {
    hooks.Approve = func(tool, preview string) (bool, string) {
      v.stopSpinner()
      v.endLine()
      ok, feedback := r.approve(tool, preview)
      v.startSpinner()

      return ok, feedback
    }
  }

  answer, err := r.Loop.Turn(turnCtx, &r.History, hooks)

  v.stopSpinner()
  if errors.Is(err, context.Canceled) && ctx.Err() == nil {
    v.endLine()
    ui.Info("interrupted")

    return nil
  }

  if err != nil {
    v.endLine()
    return err
  }

  if v.streamed {
    fmt.Println()
  } else if answer != "" {
    v.endLine()
    fmt.Println(answer)
  }

  r.maybeShowSuggestions(ctx)

  return nil
}

// approve shows a write's diff and asks. "a" approves this and every
// later write this session; any other text declines and goes back to
// the model as feedback.
func (r *REPL) approve(tool, preview string) (bool, string) {
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

  fmt.Printf(
    "%s %s ",
    ui.Yellow("apply " + tool + "?"),
    ui.Dim("[y]es / [n]o / [a]ll this session / or type feedback:"),
  )
  answer, err := r.reader.ReadString('\n')
  if err != nil {
    return false, ""
  }

  answer = strings.TrimSpace(answer)
  switch strings.ToLower(answer) {
  case "y", "yes", "":
    return true, ""
  case "a", "all":
    r.state |= flagAutoApprove

    return true, ""
  case "n", "no":
    return false, ""
  }

  return false, answer
}
