// Markdown for the terminal: Markdown renders a streamed answer as it
// arrives, line by line; RenderMarkdown renders a whole one.

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
  mdDivider = regexp.MustCompile(`^:?-+:?$`) // one cell of a | --- | row
  mdTask    = regexp.MustCompile(`^\[([ xX])\]\s+`)
)

// NewMarkdown starts a stream at the beginning of a line.
func NewMarkdown() *Markdown { return &Markdown{} }

// Write prints s: raw as it comes, each completed line redrawn
// rendered. Table rows are held until the table ends, since a column
// is only as wide as its widest cell.
func (m *Markdown) Write(s string) {
  for _, r := range s {
    if r == '\n' {
      if m.finish() {
        fmt.Print("\n")
      }

      continue
    }

    if len(m.line) == 0 && len(m.table) > 0 && Enabled {
      fmt.Print("\x1b[K") // over the held table's placeholder
    }

    m.line = append(m.line, r)
    fmt.Print(string(r))
  }
}

// Flush renders the line still open, and a held table, leaving the
// cursor after it.
func (m *Markdown) Flush() {
  if len(m.line) > 0 {
    m.finish()
  }

  if len(m.table) == 0 {
    return
  }

  rows := renderTable(m.table, tableCols(TextWidth()))
  m.table = m.table[:0]
  fmt.Print("\r\x1b[J" + strings.Join(rows, "\n"))
}

// Reset forgets a fence left open: the next answer starts clean.
func (m *Markdown) Reset() {
  m.line, m.table, m.inCode = nil, nil, false
}

// finish replaces the raw line on screen with its rendering; false
// when the line is a held table row, which leaves no row behind.
func (m *Markdown) finish() bool {
  raw := string(m.line)
  m.line = m.line[:0]

  if !Enabled {
    // No escape codes: the raw line stays, as it is markdown anyway.
    return true
  }

  erase := eraseRaw(raw)
  if !m.inCode && mdTable.MatchString(raw) {
    m.table = append(m.table, raw)
    fmt.Print(erase + Dim(fmt.Sprintf("⋯ table, %d rows", len(m.table))) + "\r")
    return false
  }

  var rows []string
  if len(m.table) > 0 {
    rows = renderTable(m.table, tableCols(TextWidth()))
    m.table = m.table[:0]
  }

  rows = append(rows, RenderLine(raw, &m.inCode, TextWidth())...)
  fmt.Print(erase + strings.Join(rows, "\n"))

  return true
}

// eraseRaw clears a raw line just printed. It began at column 0 and
// the terminal wrapped it: go back up the rows it took.
func eraseRaw(raw string) string {
  cols := termCols()
  up := 0
  if cols > 0 && len(raw) > 0 {
    up = (utf8.RuneCountInString(raw) - 1) / cols
  }

  if up > 0 {
    return fmt.Sprintf("\x1b[%dA\r\x1b[J", up)
  }

  return "\r\x1b[J"
}

// RenderMarkdown renders a whole markdown text for the terminal.
func RenderMarkdown(text string, width int) string {
  inCode := false
  var out, table []string
  for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
    if !inCode && mdTable.MatchString(line) {
      table = append(table, line)
      continue
    }

    if len(table) > 0 {
      out = append(out, renderTable(table, tableCols(width))...)
      table = nil
    }

    out = append(out, RenderLine(line, &inCode, width)...)
  }

  if len(table) > 0 {
    out = append(out, renderTable(table, tableCols(width))...)
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
    return renderTable([]string{ line }, tableCols(width))
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

// tableCols is how wide a table may be: tables aren't held to the
// prose width, only to the terminal's. 0: no limit.
func tableCols(width int) int {
  if width <= 0 {
    return 0
  }

  return max(width, termCols() - 1)
}

// renderTable lays out a table's lines in a box: each column as wide
// as its widest cell, columns shrunk to fit cols (0: no limit) with
// their cells wrapped onto more rows instead of cut. The rows above
// the divider are the header.
func renderTable(lines []string, cols int) []string {
  var rows [][][]span
  var align []byte
  header := 0
  for _, l := range lines {
    if isDivider(l) {
      if header == 0 && align == nil {
        header, align = len(rows), alignments(l)
      }

      continue
    }

    var row [][]span
    for _, c := range cells(l) {
      row = append(row, inline(c))
    }

    rows = append(rows, row)
  }

  n := 0
  for _, r := range rows {
    n = max(n, len(r))
  }

  if n == 0 {
    return []string{ Dim(strings.Repeat("─", 3)) }
  }

  for len(align) < n {
    align = append(align, 'l')
  }

  natural := make([]int, n)
  for _, r := range rows {
    for i, c := range r {
      natural[i] = max(natural[i], utf8.RuneCountInString(plain(c)))
    }
  }

  widths := natural
  if cols > 0 {
    widths = fitColumns(natural, cols - (3 * n + 1))
  }

  cellRows := make([][]string, len(rows))
  tall := false
  for i, r := range rows {
    cellRows[i] = tableRow(r, widths, align, i < header)
    tall = tall || (i >= header && len(cellRows[i]) > 1)
  }

  out := []string{ rule(widths, "┌", "┬", "┐") }
  for i, r := range cellRows {
    if i > 0 && (i == header || (i > header && tall)) {
      out = append(out, rule(widths, "├", "┼", "┤"))
    }

    out = append(out, r...)
  }

  return append(out, rule(widths, "└", "┴", "┘"))
}

// cells splits a | a | b | row; \| is a pipe inside a cell.
func cells(line string) []string {
  line = strings.ReplaceAll(strings.TrimSpace(line), `\|`, "\x00")
  line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")

  parts := strings.Split(line, "|")
  for i, p := range parts {
    parts[i] = strings.ReplaceAll(strings.TrimSpace(p), "\x00", "|")
  }

  return parts
}

// isDivider: a | --- | :-: | row, between a table's header and body.
func isDivider(line string) bool {
  for _, c := range cells(line) {
    if !mdDivider.MatchString(c) {
      return false
    }
  }

  return true
}

// alignments reads a divider's :--, :-: and --: as l, c and r.
func alignments(divider string) []byte {
  var out []byte
  for _, c := range cells(divider) {
    switch {
    case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
      out = append(out, 'c')
    case strings.HasSuffix(c, ":"):
      out = append(out, 'r')
    default:
      out = append(out, 'l')
    }
  }

  return out
}

// fitColumns shares budget columns out: a column narrower than an
// even share keeps its width, the wide ones split what's left.
func fitColumns(natural []int, budget int) []int {
  widths := make([]int, len(natural))
  total := 0
  for _, w := range natural {
    total += w
  }

  if total <= budget {
    copy(widths, natural)
    return widths
  }

  settled := make([]bool, len(natural))
  open := len(natural)
  for open > 0 {
    share := budget / open
    changed := false
    for i, w := range natural {
      if !settled[i] && w <= share {
        widths[i], settled[i] = w, true
        budget -= w
        open--
        changed = true
      }
    }

    if !changed {
      break
    }
  }

  if open == 0 {
    return widths
  }

  share, extra := budget / open, budget % open
  for i := range natural {
    if settled[i] {
      continue
    }

    widths[i] = max(1, share)
    if extra > 0 {
      widths[i]++
      extra--
    }
  }

  return widths
}

// tableRow renders one row's cells, each wrapped to its column, as
// many terminal rows as its tallest cell needs.
func tableRow(row [][]span, widths []int, align []byte, header bool) []string {
  cellLines := make([][]string, len(widths))
  height := 1
  for i, w := range widths {
    var s []span
    if i < len(row) {
      s = row[i]
    }

    if header {
      s = styled(s, Bold)
    }

    cellLines[i] = wrap(breakWords(s, w), w, "", "")
    height = max(height, len(cellLines[i]))
  }

  bar := Dim("│")
  out := make([]string, height)
  for r := range out {
    var b strings.Builder
    b.WriteString(bar)
    for i, w := range widths {
      text := ""
      if r < len(cellLines[i]) {
        text = cellLines[i][r]
      }

      b.WriteString(" " + pad(text, w, align[i]) + " " + bar)
    }

    out[r] = b.String()
  }

  return out
}

// breakWords splits words longer than width, so wrap never has to
// let one overflow its column.
func breakWords(spans []span, width int) []span {
  out := make([]span, len(spans))
  for i, s := range spans {
    words := strings.Split(s.text, " ")
    for wi, w := range words {
      r := []rune(w)
      if len(r) <= width {
        continue
      }

      var chunks []string
      for len(r) > width {
        chunks = append(chunks, string(r[:width]))
        r = r[width:]
      }

      words[wi] = strings.Join(append(chunks, string(r)), " ")
    }

    out[i] = span{ text: strings.Join(words, " "), style: s.style }
  }

  return out
}

// pad fills text out to width columns, aligned l, c or r.
func pad(text string, width int, align byte) string {
  gap := max(0, width - visibleWidth(text))
  switch align {
  case 'r':
    return strings.Repeat(" ", gap) + text
  case 'c':
    return strings.Repeat(" ", gap / 2) + text + strings.Repeat(" ", gap - gap / 2)
  }

  return text + strings.Repeat(" ", gap)
}

// rule is a table's border line: ┌───┬───┐ and the like.
func rule(widths []int, left, mid, right string) string {
  parts := make([]string, len(widths))
  for i, w := range widths {
    parts[i] = strings.Repeat("─", w + 2)
  }

  return Dim(left + strings.Join(parts, mid) + right)
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
