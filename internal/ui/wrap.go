// Word wrapping: Wrapper breaks streamed text at the terminal's width
// without splitting words or escape sequences.

package ui

import (
  "os"
  "regexp"
  "strings"
  "unicode/utf8"
)

// Text width: a narrow terminal is used whole; a wide one only to
// 75%, since long lines are hard to read. Code, tables and diffs
// aren't held to it (see Wrapper).
const (
  wrapShare = 75  // percent of a wide terminal
  wrapFull  = 100 // columns up to which the whole width is used
)

// TextWidth is the column to wrap prose at, 0 when output isn't a
// terminal (piped output is never wrapped). It's asked again each
// turn and each prompt, so resizing the window takes effect.
func TextWidth() int {
  cols := termCols()
  if cols <= 0 || !isTTY(os.Stdout) {
    return 0
  }

  return textWidth(cols)
}

func textWidth(cols int) int {
  return max(cols * wrapShare / 100, min(cols - 1, wrapFull))
}

var ansi = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")

// visibleWidth counts the columns s takes, skipping style codes.
func visibleWidth(s string) int {
  return utf8.RuneCountInString(ansi.ReplaceAllString(s, ""))
}

func NewWrapper(width int) *Wrapper {
  return &Wrapper{ Width: width, start: true }
}

// Write takes a streamed delta and returns what can be printed now.
func (w *Wrapper) Write(s string) string {
  if w.Width <= 0 {
    return s
  }

  var b strings.Builder
  for _, r := range s {
    switch {
    case r == '\n':
      w.flushWord(&b)
      b.WriteByte('\n')
      w.space = w.space[:0]
      w.col, w.indent, w.hang = 0, 0, 0
      w.start, w.raw = true, false

    case r == ' ' || r == '\t':
      if w.start && len(w.word) == 0 {
        b.WriteRune(r) // indentation prints as is
        w.col++
        w.indent++
        continue
      }

      w.flushWord(&b)
      w.space = append(w.space, r)

    default:
      w.word = append(w.word, r)
    }
  }

  return b.String()
}

// Flush returns the held-back word, for the end of a stream or
// before something else prints.
func (w *Wrapper) Flush() string {
  var b strings.Builder
  w.flushWord(&b)

  return b.String()
}

// Reset starts over at column 0, after the caller printed a newline
// of its own.
func (w *Wrapper) Reset() {
  *w = Wrapper{ Width: w.Width, fence: w.fence, word: w.word[:0], space: w.space[:0], start: true }
}

func (w *Wrapper) flushWord(b *strings.Builder) {
  if len(w.word) == 0 {
    return
  }

  if w.start {
    w.classify()
  }

  n := len(w.space) + len(w.word)
  if !w.raw && w.col + n > w.Width && w.col > w.hang {
    b.WriteByte('\n')
    b.WriteString(strings.Repeat(" ", w.hang))
    w.col = w.hang
    w.space = w.space[:0]
    n = len(w.word)
  }

  b.WriteString(string(w.space))
  b.WriteString(string(w.word))
  w.col += n
  w.space, w.word = w.space[:0], w.word[:0]
}

// classify looks at a line's first word: is it wrapped, and how far
// do its continuation lines indent?
func (w *Wrapper) classify() {
  w.start = false
  first := string(w.word)

  switch {
  case strings.HasPrefix(first, "```"), strings.HasPrefix(first, "~~~"):
    w.fence = !w.fence
    w.raw = true
  case w.fence, strings.HasPrefix(first, "|"):
    w.raw = true
  case listMarker(first):
    w.hang = w.indent + len(w.word) + 1
  case w.indent >= 4:
    w.raw = true // indented code
  default:
    w.hang = w.indent
  }
}

// listMarker: "-", "*", "+", ">", "1.", "12)".
func listMarker(s string) bool {
  switch s {
  case "-", "*", "+", ">":
    return true
  }

  if len(s) < 2 || (s[len(s) - 1] != '.' && s[len(s) - 1] != ')') {
    return false
  }

  for _, c := range s[:len(s) - 1] {
    if c < '0' || c > '9' {
      return false
    }
  }

  return true
}

// Wrap word-wraps a whole text at once.
func Wrap(s string, width int) string {
  w := NewWrapper(width)
  return w.Write(s) + w.Flush()
}

// layoutInput splits an input line into rows: the first holds
// first runes (after the prompt), the rest rest runes each. Rows
// break at a newline (which ends its row), else after a space when
// one fits, else mid-word. Every rune lands in exactly one row, so a
// rune index maps to a row and column.
func layoutInput(text []rune, first, rest int) [][]rune {
  rows := [][]rune{}
  capacity := max(first, 1)
  for {
    cut := 0
    for i := 0; i < len(text) && i <= capacity; i++ {
      if text[i] == '\n' {
        cut = i + 1
        break
      }
    }

    if cut == 0 && len(text) <= capacity {
      break
    }

    if cut == 0 {
      cut = capacity
      for i := capacity; i > capacity / 2; i-- {
        if text[i - 1] == ' ' {
          cut = i
          break
        }
      }
    }

    rows = append(rows, text[:cut])
    text = text[cut:]
    capacity = max(rest, 1)
  }

  return append(rows, text)
}

// cursorAt finds rune index i in rows: its row and column.
func cursorAt(rows [][]rune, i int) (int, int) {
  for r, row := range rows {
    if i <= len(row) && (i < len(row) || r == len(rows) - 1) {
      return r, i
    }

    i -= len(row)
  }

  return len(rows) - 1, len(rows[len(rows) - 1])
}
