package ui

import "sync"

// Composer is the input bar on the terminal's last row while a turn
// runs: what's typed there is sent with Enter, the turn's output
// scrolls in the rows above it.
type Composer struct {
  pump   *KeyPump
  send   func(string)
  hint   string
  mode   *rawState
  rows   int

  mu     sync.Mutex
  state  lineState
  queued int
  paused bool
  stop   chan struct{}
  done   chan struct{}
  pause  chan chan struct{} // a Pause, acknowledged once keys are let go
  resume chan struct{}
}
