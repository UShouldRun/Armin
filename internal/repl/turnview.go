// How a turn looks in the terminal: turnView keeps the spinner and
// its phase line (waiting, thinking, a tool's activity), streams
// thinking dimmed and the answer as Markdown, and prints one line per
// tool call with a summary of its result.

package repl

import (
  "fmt"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/ui"
)

// stream modes for turnView.
const (
  modeIdle uint8 = iota
  modeThinking
  modeAnswer
)

// Phases say who's working: the model (waiting on it, reading,
// thinking, writing a call) or a tool (agent.Activity).
const (
  phaseWaiting  = "waiting for the model"
  phaseReading  = "model is reading the prompt"
  phaseThinking = "model is thinking"
  phaseResults  = "model is reading the results"

  // stallAfter is how long the model may stay silent before the
  // status says so.
  stallAfter = 20 * time.Second
)

// maxBreak is the most newlines in a row an answer prints: one blank
// line between paragraphs, however many the model sent.
const maxBreak = 2

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

// setPhase starts a phase; watch marks phases where silence means
// the model, not a tool, is slow.
func (v *turnView) setPhase(phase string, watch bool) {
  v.mu.Lock()
  defer v.mu.Unlock()

  now := time.Now()
  v.phase, v.since, v.last, v.watch, v.count = phase, now, now, watch, 0
}

// resetClock restarts the phase's elapsed time: a tool's run starts
// once its approval is answered, not when it was asked.
func (v *turnView) resetClock() {
  v.mu.Lock()
  defer v.mu.Unlock()

  v.since, v.last = time.Now(), time.Now()
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
  } else if v.watch && v.count == 0 && time.Since(v.since) > 5 * time.Second &&
    (v.phase == phaseWaiting || v.phase == phaseResults) {
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

// drafting shows the model writing a tool call: until its
// arguments are done, nothing streams and nothing runs.
func (v *turnView) drafting(name string) {
  v.endLine()
  v.setPhase("model is writing a " + name + " call", true)
  v.startSpinner()
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

  v.setPhase(agent.Activity(name, args), false)
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
  v.setPhase(phaseResults, true)
  v.startSpinner()
}
