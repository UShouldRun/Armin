package repl

import (
  "sync/atomic"

  "armin/internal/share"
  "armin/internal/ui"
)

// Attached is a terminal joined to the project's armin instance
// (see share.Dial): no models of its own, every question and answer
// seen by all the frontends.
type Attached struct {
  client *share.Client
  events chan share.Event
  wake   chan struct{}
  inTurn atomic.Bool
  keys   *ui.KeyPump
}
