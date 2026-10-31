// The types of metrics.go.

package metrics

import (
  "sync"
)

// stat is one metric: how many times it happened and, for measured
// ones, the sum, smallest and largest value.
type stat struct {
  Count         int64
  Sum, Min, Max float64
}

// registry is the process's metrics, flushed into the cumulative file
// at shutdown.
type registry struct {
  mu    sync.Mutex
  stats map[string]*stat
}
