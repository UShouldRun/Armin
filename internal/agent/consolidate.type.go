// The types of consolidate.go.

package agent

// excerpt is one cited range, read for the prompt.
type excerpt struct {
  header string // path:start-end, as the claim must cite it
  text   string
}
