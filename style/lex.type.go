// The types of lex.go.

package main

// srcLine is one line of a file: as written, and as code with the
// insides of strings, regexes and comments blanked.
type srcLine struct {
  text, code string

  // opener is the line that opened the bracket this line's first
  // closer closes, -1 when the line doesn't start with a closer
  opener int

  // inText: the line starts inside a multi-line raw string or
  // template literal, so it is text, not code
  inText bool
}
