package ui

import (
  "fmt"
  "regexp"
  "strings"
  "unicode/utf8"
)

var (
  mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
  mdRule    = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
  mdBullet  = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)
  mdNumber  = regexp.MustCompile(`^(\s*)(\d+[.)])\s+(.*)$`)
  mdQuote   = regexp.MustCompile(`^\s*>\s?(.*)$`)
  mdTable   = regexp.MustCompile(`^\s*\|.*\|\s*$`)
  mdDivider = regexp.MustCompile(`^\s*\|?(\s*:?-{2,}:?\s*\|)+\s*:?-*:?\s*$`)
  mdTask    = regexp.MustCompile(`^\[([ xX])\]\s+`)
)

// NewMarkdown starts a stream at the beginning of a line.
func NewMarkdown() *Markdown { return &Markdown{} }

// Write prints s: raw as it comes, each completed line redrawn
// rendered.
func (m *Markdown) Write(s string) {
  for _, r := range s {
    if r == '\n' {
      m.finish()
      fmt.Print("\n")
      continue
    }

    m.line = append(m.line, r)
    fmt.Print(string(r))
  }
}

// Flush renders the line still open, leaving the cursor after it.
func (m *Markdown) Flush() {
  if len(m.line) > 0 {
    m.finish()
  }
}

// Reset forgets a fence left open: the next answer starts clean.
func (m *Markdown) Reset() {
  m.line, m.inCode = nil, false
}

// finish replaces the raw line on screen with its rendering. The raw
// line began at column 0 and the terminal wrapped it: go back up the
// rows it took, then draw over them.
func (m *Markdown) finish() {
  raw := string(m.line)
  m.line = m.line[:0]

  rows := RenderLine(raw, &m.inCode, TextWidth())
  cols := termCols()
  up := 0
  if cols > 0 && len(raw) > 0 {
    up = (utf8.RuneCountInString(raw) - 1) / cols
  }

  if !Enabled {
    // No escape codes: the raw line stays, as it is markdown anyway.
    return
  }

  if up > 0 {
    fmt.Printf("\x1b[%dA", up)
  }

  fmt.Print("\r\x1b[J" + strings.Join(rows, "\n"))
}

// RenderMarkdown renders a whole markdown text for the terminal.
func RenderMarkdown(text string, width int) string {
  inCode := false
  var out []string
  for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
    out = append(out, RenderLine(line, &inCode, width)...)
  }

  return strings.Join(out, "\n")
}

// RenderLine renders one markdown line as terminal rows, wrapped at
// width (0: no wrapping). inCode carries fenced code across lines.
func RenderLine(line string, inCode *bool, width int) []string {
  trimmed := strings.TrimSpace(line)
  if strings.HasPrefix(trimmed, "```") {
    *inCode = !*inCode
    if *inCode {
      lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
      return []string{ Dim("┌─" + strings.TrimRight(" " + lang, " ")) }
    }

    return []string{ Dim("└─") }
  }

  if *inCode {
    return []string{ Dim("│ ") + Cyan(line) }
  }

  if trimmed == "" {
    return []string{ "" }
  }

  m := mdHeading.FindStringSubmatch(line)
  if m != nil {
    text := plain(inline(m[2]))
    if len(m[1]) <= 2 {
      return []string{ Bold(Magenta(text)) }
    }

    return []string{ Bold(text) }
  }

  if mdRule.MatchString(line) {
    return []string{ Dim(strings.Repeat("─", max(3, min(width, 60)))) }
  }

  if mdTable.MatchString(line) {
    return []string{ table(line, width) }
  }

  m = mdQuote.FindStringSubmatch(line)
  if m != nil {
    return wrap(inline(m[1]), width, Dim("│ "), Dim("│ "))
  }

  m = mdBullet.FindStringSubmatch(line)
  if m != nil {
    indent := strings.Repeat(" ", len(m[1]))
    text, mark := m[2], "• "
    task := mdTask.FindStringSubmatch(text)
    if task != nil {
      mark = "☐ "
      if task[1] != " " {
        mark = Green("✓ ")
      }

      text = text[len(task[0]):]
    }

    return wrap(inline(text), width, indent + mark, indent + "  ")
  }

  m = mdNumber.FindStringSubmatch(line)
  if m != nil {
    indent := strings.Repeat(" ", len(m[1]))
    hang := strings.Repeat(" ", utf8.RuneCountInString(m[2]) + 1)

    return wrap(inline(m[3]), width, indent + m[2] + " ", indent + hang)
  }

  return wrap(inline(line), width, "", "")
}

// table renders a | a | b | row with ruled separators; a divider
// row becomes a line. Rows aren't wrapped: cut to the terminal.
func table(line string, width int) string {
  if mdDivider.MatchString(line) {
    return Dim(strings.Repeat("─", max(3, min(width, visibleWidth(line)))))
  }

  cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
  parts := make([]string, len(cells))
  for i, c := range cells {
    parts[i] = render(inline(strings.TrimSpace(c)))
  }

  return Fit(strings.Join(parts, Dim(" │ ")), termCols() - 1)
}

// inline splits text into styled spans: `code`, **bold**, *italic*,
// ~~strike~~ and [links](url).
func inline(text string) []span {
  var out []span
  var b strings.Builder
  flush := func() {
    if b.Len() > 0 {
      out = append(out, span{ text: b.String() })
      b.Reset()
    }
  }

  r := []rune(text)
  for i := 0; i < len(r); i++ {
    rest := string(r[i:])
    switch {
    case r[i] == '`':
      j := i + 1
      for j < len(r) && r[j] != '`' {
        j++
      }

      if j == len(r) {
        break
      }

      flush()
      out = append(out, span{ text: string(r[i + 1:j]), style: Cyan })
      i = j
      continue

    case strings.HasPrefix(rest, "**") || strings.HasPrefix(rest, "__"):
      body, n := delimited(r, i, string(r[i:i + 2]))
      if n == 0 {
        break
      }

      flush()
      out = append(out, styled(inline(body), Bold)...)
      i += n - 1
      continue

    case strings.HasPrefix(rest, "~~"):
      body, n := delimited(r, i, "~~")
      if n == 0 {
        break
      }

      flush()
      out = append(out, styled(inline(body), Strike)...)
      i += n - 1
      continue

    case (r[i] == '*' || r[i] == '_') && wordStart(r, i):
      body, n := delimited(r, i, string(r[i]))
      if n == 0 || strings.HasPrefix(body, " ") {
        break
      }

      flush()
      out = append(out, styled(inline(body), Italic)...)
      i += n - 1
      continue

    case r[i] == '[' && !strings.HasPrefix(rest, "[["):
      close := strings.Index(rest, "](")
      if close < 0 {
        break
      }

      end := strings.IndexRune(rest[close:], ')')
      if end < 0 {
        break
      }

      label, url := rest[1:close], rest[close + 2:close + end]
      flush()
      out = append(out, span{ text: label, style: Underline })
      if url != label {
        out = append(out, span{ text: " (" + url + ")", style: Dim })
      }

      i += utf8.RuneCountInString(rest[:close + end])
      continue
    }

    b.WriteRune(r[i])
  }

  flush()

  return out
}

// delimited finds the text between the delimiter at r[i:] and its
// closing twin; n is the runes consumed, 0 when it doesn't close.
func delimited(r []rune, i int, delim string) (body string, n int) {
  d := utf8.RuneCountInString(delim)
  rest := string(r[i + d:])
  end := strings.Index(rest, delim)
  if end <= 0 {
    return "", 0
  }

  body = rest[:end]

  return body, d * 2 + utf8.RuneCountInString(body)
}

// wordStart: an _ or * that opens emphasis, not one inside a word
// (snake_case, a*b).
func wordStart(r []rune, i int) bool {
  if i + 1 >= len(r) || r[i + 1] == ' ' {
    return false
  }

  if i == 0 {
    return true
  }

  prev := r[i - 1]

  return prev == ' ' || prev == '(' || prev == '"'
}

func styled(spans []span, style func(string) string) []span {
  for i := range spans {
    inner := spans[i].style
    if inner == nil {
      spans[i].style = style
      continue
    }

    spans[i].style = func(s string) string { return style(inner(s)) }
  }

  return spans
}

func plain(spans []span) string {
  var b strings.Builder
  for _, s := range spans {
    b.WriteString(s.text)
  }

  return b.String()
}

func render(spans []span) string {
  var b strings.Builder
  for _, s := range spans {
    if s.style == nil {
      b.WriteString(s.text)
      continue
    }

    b.WriteString(s.style(s.text))
  }

  return b.String()
}

// wrap lays spans out in rows of width columns, word by word, each
// word keeping its style; first starts the first row, hang the rest.
func wrap(spans []span, width int, first, hang string) []string {
  if width <= 0 {
    return []string{ first + render(spans) }
  }

  var rows []string
  row, used := first, visibleWidth(first)
  start := used
  for _, s := range spans {
    words := strings.Split(s.text, " ")
    for wi, w := range words {
      if wi > 0 && used > start {
        if used + 1 > width {
          rows = append(rows, row)
          row, used = hang, visibleWidth(hang)
          start = used
        } else {
          row += " "
          used++
        }
      }

      if w == "" {
        continue
      }

      n := utf8.RuneCountInString(w)
      if used + n > width && used > start {
        rows = append(rows, row)
        row, used = hang, visibleWidth(hang)
        start = used
      }

      if s.style != nil {
        w = s.style(w)
      }

      row += w
      used += n
    }
  }

  return append(rows, row)
}
