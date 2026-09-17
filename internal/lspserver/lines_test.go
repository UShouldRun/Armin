package lspserver

import (
  "strconv"
  "testing"
)

const sampleDoc = "line0\nline1\nline2\nline3\nline4\n"

func TestLineOffset(t *testing.T) {
  cases := []struct {
    name      string
    line      int
    wantStart int
    wantEnd   int
    wantOK    bool
  }{
    {"first line", 0, 0, 5, true},
    {"middle line", 2, 12, 17, true},
    {"negative line", -1, 0, 0, false},
    {"line past end", 99, 0, 0, false},
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      start, end, ok := lineOffset(sampleDoc, c.line)
      if ok != c.wantOK {
        t.Fatalf("ok = %v, want %v", ok, c.wantOK)
      }
      if !ok {
        return
      }
      if start != c.wantStart || end != c.wantEnd {
        t.Fatalf(
          "lineOffset(_, %d) = (%d, %d), want (%d, %d)",
          c.line, start, end, c.wantStart, c.wantEnd,
        )
      }
      want := "line" + strconv.Itoa(c.line)
      if got := sampleDoc[start:end]; got != want {
        t.Fatalf("text[start:end] = %q, want %q", got, want)
      }
    })
  }
}

func TestLineOffsetLastLineNoTrailingNewline(t *testing.T) {
  text := "a\nb\nc" // no trailing newline
  start, end, ok := lineOffset(text, 2)
  if !ok || text[start:end] != "c" {
    t.Fatalf("lineOffset last line = (%d,%d,%v), want the final \"c\"", start, end, ok)
  }
}

func TestContextWindow(t *testing.T) {
  snippet, lineText := contextWindow(sampleDoc, 2, 1)
  if lineText != "line2" {
    t.Fatalf("lineText = %q, want line2", lineText)
  }
  want := "line1\nline2\nline3"
  if snippet != want {
    t.Fatalf("snippet = %q, want %q", snippet, want)
  }
}

func TestContextWindowClampsAtDocumentEdges(t *testing.T) {
  snippet, lineText := contextWindow(sampleDoc, 0, 3)
  if lineText != "line0" {
    t.Fatalf("lineText = %q, want line0", lineText)
  }
  if snippet != "line0\nline1\nline2\nline3" {
    t.Fatalf("snippet at start-of-doc = %q", snippet)
  }

  snippet, lineText = contextWindow(sampleDoc, 4, 3)
  if lineText != "line4" {
    t.Fatalf("lineText = %q, want line4", lineText)
  }
  if snippet != "line1\nline2\nline3\nline4\n" {
    t.Fatalf("snippet at end-of-doc = %q", snippet)
  }
}

func TestContextWindowOutOfRange(t *testing.T) {
  snippet, lineText := contextWindow(sampleDoc, 99, 2)
  if snippet != "" || lineText != "" {
    t.Fatalf("out-of-range window = (%q, %q), want empty", snippet, lineText)
  }
}

// TestContextWindowAllocationsStayBounded is the "10-year-old laptop"
// check for this specific fix: contextWindow used to strings.Split the
// entire document into one string per line just to look at a handful
// near the cursor. On a real file, that's thousands of allocations per
// hover. It must now cost roughly the same however long the document
// is — this generates a large document and asserts a small, fixed
// allocation ceiling rather than trusting the implementation by eye.
func TestContextWindowAllocationsStayBounded(t *testing.T) {
  var b []byte
  for i := 0; i < 20000; i++ {
    b = append(b, []byte("some line of source code here\n")...)
  }
  big := string(b)

  const allocLimit = 4 // two string headers out (snippet, lineText) plus slack
  allocs := testing.AllocsPerRun(20, func() {
    contextWindow(big, 10000, 6)
  })
  if allocs > allocLimit {
    t.Fatalf(
      "contextWindow on a 20000-line document allocated %.0f times, want <= %d "+
        "(this is the strings.Split-per-line regression check)",
      allocs, allocLimit,
    )
  }
}
