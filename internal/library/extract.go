package library

import (
  "bytes"
  "context"
  "fmt"
  "html"
  "os"
  "os/exec"
  "strings"
  "time"
  "unicode"
  "unicode/utf8"
)

const (
  // pdfTimeout bounds one pdftotext run; the largest manuals (Intel's
  // 5000 pages) take about a minute.
  pdfTimeout = 10 * time.Minute

  // windowLines and windowChars split long sections, so a passage
  // stays small enough to show and to score fairly.
  windowLines = 60
  windowChars = 3000
)

// extract reads one document into pieces.
func extract(ctx context.Context, path string, kind Kind) ([]piece, error) {
  if kind == kindPDF {
    return extractPDF(ctx, path)
  }

  data, err := os.ReadFile(path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in extract:\n\t%w", err)
  }

  if bytes.IndexByte(data[:min(len(data), 1024)], 0) >= 0 {
    return nil, nil // binary
  }

  if kind == kindHTML {
    return extractHTML(string(data)), nil
  }

  return extractLines(string(data), kind), nil
}

// extractPDF is one piece per page, through poppler's pdftotext (in
// reading order, not -layout: search wants sentences, not columns).
func extractPDF(ctx context.Context, path string) ([]piece, error) {
  ctx, cancel := context.WithTimeout(ctx, pdfTimeout)
  defer cancel()

  cmd := exec.CommandContext(
    ctx, "pdftotext", "-enc", "UTF-8", "-q", path, "-",
  )
  out, err := cmd.Output()
  if err != nil && len(out) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in extractPDF:\n\t pdftotext %s: %w", path, err,
    )
  }

  var pieces []piece
  pages := strings.Split(string(out), "\f")
  for i, p := range pages {
    text := squeeze(p)
    if text == "" {
      continue
    }

    pieces = append(pieces, piece{ Loc: uint32(i + 1), Text: text })
  }

  return pieces, nil
}

// extractHTML cuts a page at its h1-h3 headings. Scripts, styles and
// comments are dropped, tags become spaces. Loc is the heading's
// line in the file, for read_file.
func extractHTML(src string) []piece {
  var (
    pieces  []piece
    text    strings.Builder
    label   string
    loc     = uint32(1)
    line    = uint32(1)
    heading = false
    head    strings.Builder
  )

  flush := func() {
    t := squeeze(html.UnescapeString(text.String()))
    if t != "" {
      pieces = append(pieces, windows(label, loc, t)...)
    }

    text.Reset()
  }

  for i := 0; i < len(src); {
    c := src[i]
    if c == '\n' {
      line++
    }

    if c != '<' {
      if heading {
        head.WriteByte(c)
      } else {
        text.WriteByte(c)
      }

      i++
      continue
    }

    end := strings.IndexByte(src[i:], '>')
    if end < 0 {
      break
    }

    tag := strings.ToLower(src[i + 1:i + end])
    skipTo := ""
    switch {
    case strings.HasPrefix(tag, "!--"):
      skipTo = "-->"
    case strings.HasPrefix(tag, "script"):
      skipTo = "</script>"
    case strings.HasPrefix(tag, "style"):
      skipTo = "</style>"
    }

    if skipTo != "" {
      j := strings.Index(strings.ToLower(src[i:]), skipTo)
      if j < 0 {
        break
      }

      line += uint32(strings.Count(src[i:i + j], "\n"))
      i += j + len(skipTo)
      continue
    }

    line += uint32(strings.Count(src[i:i + end], "\n"))
    i += end + 1

    if isHeading(tag) {
      flush()
      heading, loc = true, line
      head.Reset()
      continue
    }

    if heading && strings.HasPrefix(tag, "/h") {
      heading = false
      label = squeeze(html.UnescapeString(head.String()))
      text.WriteString(label + ". ")
      continue
    }

    if heading {
      head.WriteByte(' ')
    } else {
      text.WriteByte(' ')
    }
  }

  flush()

  return pieces
}

func isHeading(tag string) bool {
  return len(tag) >= 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '3' &&
    (len(tag) == 2 || tag[2] == ' ' || tag[2] == '\t' || tag[2] == '\n')
}

// extractLines cuts Markdown, reStructuredText, man pages, go doc
// output and code at their headings, and long sections into
// windows. Loc is the first line, for read_file.
func extractLines(src string, kind Kind) []piece {
  var (
    pieces []piece
    body   strings.Builder
    label  string
    start  = uint32(1)
    n      = 0
    prev   string
  )

  flush := func(next uint32) {
    t := squeeze(body.String())
    if t != "" {
      pieces = append(pieces, piece{ Label: label, Loc: start, Text: t })
    }

    body.Reset()
    start, n = next, 0
  }

  line := uint32(0)
  for len(src) > 0 {
    var l string
    l, src, _ = strings.Cut(src, "\n")
    line++
    trimmed := strings.TrimSpace(l)

    h, underline := headingOf(trimmed, prev, kind)
    switch {
    case underline:
      // the previous line was the heading: it's already in body
      label = h
    case h != "":
      flush(line)
      label = h
    case n >= windowLines || body.Len() >= windowChars:
      flush(line)
    }

    body.WriteString(l)
    body.WriteByte('\n')
    n++
    prev = trimmed
  }

  flush(line)

  return pieces
}

// headingOf recognizes a heading line: "# Title" in Markdown, a
// "====" or "----" underline (reStructuredText, setext) under a text
// line, or an all-caps line such as a man page's "DESCRIPTION".
func headingOf(line, prev string, kind Kind) (string, bool) {
  if line == "" {
    return "", false
  }

  if kind == kindMarkdown {
    if line[0] == '#' {
      return strings.TrimSpace(strings.TrimLeft(line, "#")), false
    }

    if prev != "" && len(line) >= 3 && strings.Trim(line, "=-~^") == "" {
      return prev, true
    }

    return "", false
  }

  if len(line) < 3 || len(line) > 60 {
    return "", false
  }

  letters := 0
  for _, r := range line {
    if unicode.IsLower(r) {
      return "", false
    }

    if unicode.IsUpper(r) {
      letters++
    }
  }

  if letters < 3 {
    return "", false
  }

  return line, false
}

// windows splits a long passage at roughly windowChars, on spaces.
func windows(label string, loc uint32, text string) []piece {
  var pieces []piece
  for len(text) > windowChars {
    cut := strings.LastIndexByte(text[:windowChars], ' ')
    if cut < windowChars / 2 {
      cut = windowChars
    }

    pieces = append(pieces, piece{ Label: label, Loc: loc, Text: text[:cut] })
    text = strings.TrimSpace(text[cut:])
  }

  if text != "" {
    pieces = append(pieces, piece{ Label: label, Loc: loc, Text: text })
  }

  return pieces
}

// squeeze collapses runs of whitespace (and PDF hyphenation at line
// ends) into single spaces.
func squeeze(s string) string {
  var b strings.Builder
  b.Grow(len(s))
  space := false
  for i := 0; i < len(s); i++ {
    c := s[i]
    broken := i > 0 && i + 1 < len(s) && s[i + 1] == '\n'
    if c == '-' && broken && isWordByte(s[i - 1]) {
      i++ // "exam-\nple" → "example"
      continue
    }

    if c == ' ' || c == '\n' || c == '\t' || c == '\r' || c == '\f' ||
      c == '\v' {
      space = true
      continue
    }

    if space && b.Len() > 0 {
      b.WriteByte(' ')
    }

    space = false
    b.WriteByte(c)
  }

  return b.String()
}

func isWordByte(c byte) bool {
  return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// stopwords are too common to help a search; leaving them out of
// the index makes it a good deal smaller.
var stopwords = map[string]bool{
  "the": true, "of": true, "and": true, "to": true, "in": true,
  "is": true, "it": true, "that": true, "for": true, "as": true,
  "on": true, "with": true, "be": true, "by": true, "this": true,
  "are": true, "or": true, "an": true, "at": true, "from": true,
  "which": true, "we": true, "can": true, "if": true, "not": true,
  "was": true, "but": true, "have": true, "has": true, "will": true,
  "their": true, "there": true, "these": true, "they": true, "its": true,
  "you": true, "your": true, "all": true, "also": true, "so": true,
}

// eachTerm calls emit with every searchable term of s: lowercase
// runs of letters, digits and "_", 2 to 32 bytes, no stopwords, a
// plural "s" dropped. Allocation-free for ASCII text.
func eachTerm(s string, buf []byte, emit func(term []byte)) {
  buf = buf[:0]
  for i := 0; i <= len(s); {
    if i < len(s) {
      c := s[i]
      if c < utf8.RuneSelf {
        if c >= 'A' && c <= 'Z' {
          c += 'a' - 'A'
        }

        if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
          buf = append(buf, c)
          i++
          continue
        }

        i++
      } else {
        r, size := utf8.DecodeRuneInString(s[i:])
        if unicode.IsLetter(r) || unicode.IsDigit(r) {
          buf = utf8.AppendRune(buf, unicode.ToLower(r))
          i += size
          continue
        }

        i += size
      }
    } else {
      i++
    }

    if len(buf) >= 2 && len(buf) <= 32 && keep(buf) {
      emit(stem(buf))
    }

    buf = buf[:0]
  }
}

func keep(t []byte) bool {
  if stopwords[string(t)] {
    return false
  }

  digits := 0
  for _, c := range t {
    if c >= '0' && c <= '9' {
      digits++
    }
  }

  return digits < 7 // long numbers: page refs, hashes, tables
}

// stem drops a plural "s": "mutexes" and "mutex" still differ, but
// "threads" finds "thread". The same on both sides, so consistent.
func stem(t []byte) []byte {
  n := len(t)
  if n <= 4 || t[n - 1] != 's' {
    return t
  }

  switch t[n - 2] {
  case 's', 'u', 'i': // class, status, analysis
    return t
  }

  return t[:n - 1]
}

// queryTerms is eachTerm for a query, without duplicates.
func queryTerms(q string) []string {
  var out []string
  seen := map[string]bool{}
  eachTerm(q, make([]byte, 0, 32), func(t []byte) {
    s := string(t)
    if !seen[s] {
      seen[s] = true
      out = append(out, s)
    }
  })

  return out
}
