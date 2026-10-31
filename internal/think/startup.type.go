// The types of startup.go.

package think

import (
  "time"
)

// Stage is one timed step of starting armin.
type Stage struct {
  Name string
  Took time.Duration
}
