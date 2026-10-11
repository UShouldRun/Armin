package ui

import (
  "fmt"
  "sync"
  "time"
)

// frames is a braille-dot spinner — compact, doesn't jitter the
// line width, reads fine on any terminal that does UTF-8 (which is
// to say: any terminal worth targeting in 2026).
var frames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner is a single-line "thinking" indicator, live while we're
// waiting on the model's first token or a slow tool call. It's a
// no-op when Enabled is false (non-TTY, NO_COLOR, piped output) so
// scripted use of armin never has to deal with stray control chars.
type Spinner struct {
  label string
  stop  chan struct{}
  done  sync.WaitGroup
}

// NewSpinner starts a spinner immediately with the given label.
func NewSpinner(label string) *Spinner {
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
      fmt.Printf("\r%s %s", frame, Dim(s.label))
      i++
    }
  }
}

// Stop clears the spinner line. Safe to call once; calling it more
// than once will block, so callers own exactly one Stop per
// NewSpinner.
func (s *Spinner) Stop() {
  close(s.stop)
  s.done.Wait()
}
