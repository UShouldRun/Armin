// A turn's view state: phase, stream mode, held lines.

package repl

import (
  "sync"
  "time"

  "armin/internal/ui"
)

// turnView owns the terminal during one turn: the spinner, streamed
// thinking (dim) and answer text, tool-call lines and approvals.
type turnView struct {
  mu    sync.Mutex // status fields: the spinner reads them every frame
  phase string
  since time.Time // when the phase began
  last  time.Time // last sign of life from the model
  watch bool      // the phase waits on the model
  count int       // hidden-thinking chunks this phase

  spinner      *ui.Spinner
  wrap         *ui.Wrapper // prose at ui.TextWidth
  tips         []string    // rotate in the status; nil shows none
  tipBase      uint32
  start        time.Time   // when the turn began
  mode         uint8
  showThinking bool
  streamed     bool
  pending      int // newlines the answer ended on so far, held back
  md           *ui.Markdown // renders the answer as it streams
  held         string       // a write's or run's call line, until its proposal shows
}
