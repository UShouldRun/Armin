package ui

// Markdown prints a streamed markdown answer: each line shows raw
// while it arrives, then is redrawn rendered once its newline does.
type Markdown struct {
  line   []rune // the current line, as received
  inCode bool   // inside a ``` fence
}

// span is a run of inline text with one style.
type span struct {
  text  string
  style func(string) string // nil: plain
}
