// The types of report.go.

package metrics

// rate is a derived figure: how often num happened per den.
type rate struct {
  label, num, den string
}
