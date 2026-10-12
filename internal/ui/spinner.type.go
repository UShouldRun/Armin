package ui

import (
  "sync"
)

// Spinner is a single-line "thinking" indicator, live while we're
// waiting on the model's first token or a slow tool call. It's a
// no-op when Enabled is false (non-TTY, NO_COLOR, piped output) so
// scripted use of armin never has to deal with stray control chars.
type Spinner struct {
  label func() string
  stop  chan struct{}
  done  sync.WaitGroup
}
