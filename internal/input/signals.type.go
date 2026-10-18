// The signal router's handlers, swapped atomically as frontends start.

package input

import (
  "sync/atomic"
)

// Signals routes Ctrl-C, SIGTERM and SIGHUP to whoever runs now.
type Signals struct {
  interrupt atomic.Pointer[func() bool]
  terminate atomic.Pointer[func()]
}
