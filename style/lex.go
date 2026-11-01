// Lexing just enough of Go and JavaScript to tell code from text: the
// insides of "strings", 'runes', `raw strings` (which span lines),
// JS /regexes/, // and /* */ comments are blanked to spaces, so a
// rule about code never fires on what a string says. Columns stay
// where they were.

package main

import "strings"

// lexLines splits src into lines, each with its code-only twin.
func lexLines(src string, js bool) []srcLine {
  var out []srcLine
  code := []byte(src)
  quote := byte(0)   // the open string's quote, 0 outside one
  quoteAt := -1      // where it opened
  block := false     // inside /* */
  regex, cls := false, false
  lineStart, inText := 0, false
  last := byte(0)    // the last code byte that isn't a space

  flush := func(end int) {
    // a raw string still open at the line's end is text from its
    // opening quote on
    if quote == '`' && quoteAt >= lineStart {
      code[quoteAt] = ' '
    }

    l := srcLine{
      text:   src[lineStart:end],
      code:   string(code[lineStart:end]),
      inText: inText,
    }
    out = append(out, l)
    lineStart = end + 1
    inText = quote == '`'
  }

  for i := 0; i < len(src); i++ {
    c := src[i]
    if c == '\n' {
      flush(i)
      continue
    }

    switch {
    case block:
      if c == '*' && i + 1 < len(src) && src[i + 1] == '/' {
        block = false
        i++
      } else {
        code[i] = ' '
      }

    case regex:
      switch {
      case c == '\\':
        code[i] = ' '
        if i + 1 < len(src) && src[i + 1] != '\n' {
          i++
          code[i] = ' '
        }

      case c == '[':
        cls = true
        code[i] = ' '
      case c == ']':
        cls = false
        code[i] = ' '
      case c == '/' && !cls:
        regex = false
        last = '/'
      default:
        code[i] = ' '
      }

    case quote != 0:
      switch {
      case c == '\\' && quote != '`':
        code[i] = ' '
        if i + 1 < len(src) && src[i + 1] != '\n' {
          i++
          code[i] = ' '
        }

      case c == quote:
        quote = 0
        last = c
      default:
        code[i] = ' '
      }

    case c == '"' || c == '\'' || c == '`':
      quote, quoteAt = c, i
    case c == '/' && i + 1 < len(src) && src[i + 1] == '/':
      for i < len(src) && src[i] != '\n' {
        code[i] = ' '
        i++
      }

      i--
    case c == '/' && i + 1 < len(src) && src[i + 1] == '*':
      block = true
      code[i], code[i + 1] = ' ', ' '
      i++
    case c == '/' && js && strings.IndexByte("(,=:[!&|?{};+-*%<>~^", last) >= 0:
      regex = true
    default:
      if c != ' ' && c != '\t' {
        last = c
      }
    }
  }

  if lineStart <= len(src) {
    flush(len(src))
  }

  openers(out)

  return out
}

// openers sets each line's opener: for a line whose code starts with
// a closing bracket, the line that opened it.
func openers(lines []srcLine) {
  var stack []int
  for i := range lines {
    lines[i].opener = -1
    code := lines[i].code
    first := len(code) - len(strings.TrimLeft(code, " \t"))
    for j := 0; j < len(code); j++ {
      switch code[j] {
      case '(', '[', '{':
        stack = append(stack, i)
      case ')', ']', '}':
        if len(stack) == 0 {
          continue
        }

        if j == first {
          lines[i].opener = stack[len(stack) - 1]
        }

        stack = stack[:len(stack) - 1]
      }
    }
  }
}
