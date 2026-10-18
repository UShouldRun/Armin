// The types of trace.go.

package ui

// traceFrame is one wrapper's part of a chain: its function and the
// words it added (often none: a bare %w).
type traceFrame struct {
  fn, text string
}
