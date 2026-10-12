package ui

import (
  "fmt"
  "time"
)

// frames is a braille-dot spinner — compact, doesn't jitter the
// line width, reads fine on any terminal that does UTF-8 (which is
// to say: any terminal worth targeting in 2026).
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// NewSpinner starts a spinner immediately with the given label and
// the time since it started.
func NewSpinner(label string) *Spinner {
  start := time.Now()
  return NewStatus(func() string { return label + " · " + Since(start) })
}

// NewStatus starts a spinner whose text is asked for on every frame,
// so it can show a live status (phase, elapsed time, counts).
func NewStatus(label func() string) *Spinner {
  s := &Spinner{ label: label, stop: make(chan struct{}) }
  if !Enabled {
    return s
  }

  s.done.Add(1)
  go s.run()

  return s
}

func (s *Spinner) run() {
  defer s.done.Done()

  ticker := time.NewTicker(80 * time.Millisecond)
  defer ticker.Stop()

  i := 0
  for {
    select {
    case <-s.stop:
      fmt.Print("\r\x1b[K")

      return
    case <-ticker.C:
      frame := Cyan(frames[i % len(frames)])
      label := fitLine(s.label(), termCols() - 3) // frame, space, a spare column
      fmt.Printf("\r\x1b[K%s %s", frame, Dim(label))
      i++
    }
  }
}

// Since is the time since t, short: "8s", "2m05s".
func Since(t time.Time) string {
  d := time.Since(t).Round(time.Second)
  if d < time.Minute {
    return fmt.Sprintf("%ds", int(d.Seconds()))
  }

  return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds()) % 60)
}

// Stop clears the spinner line. Safe to call once; calling it more
// than once will block, so callers own exactly one Stop per
// NewSpinner.
func (s *Spinner) Stop() {
  close(s.stop)
  s.done.Wait()
}

// Fit cuts s to width columns, ending in "…" when it had to; for
// lines that must stay on one row.
func Fit(s string, width int) string { return fitLine(s, width) }

// fitLine cuts s to width columns, ending in "…" when it had to.
// The spinner redraws with \r, which only returns to the start of
// the row it's on: a status that wrapped would leave its first row
// behind on every frame. width <= 0 (no terminal) leaves s alone.
func fitLine(s string, width int) string {
  if width <= 0 {
    return s
  }

  r := []rune(s)
  if len(r) <= width {
    return s
  }

  if width == 1 {
    return "…"
  }

  return string(r[:width - 1]) + "…"
}
