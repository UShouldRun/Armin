// The types of markdown.go.

package ui

// Markdown prints a streamed markdown answer: each line shows raw
// while it arrives, then is redrawn rendered once its newline does.
// A table shows as one placeholder row until it ends, then whole.
type Markdown struct {
  line   []rune   // the current line, as received
  table  []string // table rows held until the table ends
  inCode bool     // inside a ``` fence
}

// span is a run of inline text with one style.
type span struct {
  text  string
  style func(string) string // nil: plain
}
