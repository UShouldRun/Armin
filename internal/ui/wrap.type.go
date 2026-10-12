package ui

// Wrapper word-wraps streamed markdown as it arrives. It holds back
// at most one word, and decides per line, from the line's first
// word, whether the line wraps:
//
//   - prose wraps at Width, list items with a hanging indent under
//     their text;
//   - code fences (and everything inside), table rows and lines
//     indented as code don't: breaking them would change what they
//     mean, so they run to the terminal's own edge.
type Wrapper struct {
  Width int

  word  []rune // pending, not yet printed
  space []rune // whitespace between the printed text and word

  col    int // visible column of the cursor
  indent int // leading spaces of the current line
  hang   int // indent for the current line's continuations

  start bool // nothing but indentation printed on this line yet
  raw   bool // this line isn't wrapped
  fence bool // inside a ``` block
}
