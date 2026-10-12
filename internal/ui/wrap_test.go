package ui

import (
  "strings"
  "testing"
)

func TestTextWidth(t *testing.T) {
  cases := map[int]int{ 60: 59, 90: 89, 120: 100, 200: 150, 300: 225 }
  for cols, want := range cases {
    got := textWidth(cols)
    if got != want {
      t.Errorf("textWidth(%d) = %d, want %d", cols, got, want)
    }
  }
}

// streamed feeds s a few bytes at a time, like model tokens.
func streamed(s string, width int) string {
  w := NewWrapper(width)
  var b strings.Builder
  for len(s) > 0 {
    n := min(3, len(s))
    b.WriteString(w.Write(s[:n]))
    s = s[n:]
  }

  return b.String() + w.Flush()
}

func TestWrapperWrapsProseOnly(t *testing.T) {
  in := "The quick brown fox jumps over the lazy dog again and again.\n" +
    "- a list item that is long enough to wrap around\n" +
    "| col one | col two | col three | col four | col five |\n" +
    "```go\n" +
    "func veryLongFunctionName(argumentOne, argumentTwo int) {}\n" +
    "```\n" +
    "short\n"

  got := streamed(in, 20)
  want := "The quick brown fox\n" +
    "jumps over the lazy\n" +
    "dog again and again.\n" +
    "- a list item that\n" +
    "  is long enough to\n" +
    "  wrap around\n" +
    "| col one | col two | col three | col four | col five |\n" +
    "```go\n" +
    "func veryLongFunctionName(argumentOne, argumentTwo int) {}\n" +
    "```\n" +
    "short\n"

  if got != want {
    t.Fatalf("got:\n%s\nwant:\n%s", got, want)
  }

  if streamed(in, 0) != in {
    t.Fatal("width 0 must pass text through")
  }
}

func TestWrapperKeepsIndentedCodeAndLongWords(t *testing.T) {
  in := "    indented code line that is much longer than twenty\n" +
    "see https://example.com/a/very/long/path/that/cannot/break ok\n"

  got := streamed(in, 20)
  want := "    indented code line that is much longer than twenty\n" +
    "see\n" +
    "https://example.com/a/very/long/path/that/cannot/break\n" +
    "ok\n"

  if got != want {
    t.Fatalf("got:\n%q\nwant:\n%q", got, want)
  }
}

func TestLayoutInputAndCursor(t *testing.T) {
  text := []rune("fix the bug in the parser please")
  rows := layoutInput(text, 10, 12)

  joined := ""
  for _, r := range rows {
    if len(r) > 12 {
      t.Fatalf("row %q is wider than 12", string(r))
    }

    joined += string(r)
  }

  if joined != string(text) || len(rows[0]) > 10 {
    t.Fatalf("rows = %q", rows)
  }

  r, c := cursorAt(rows, len(text))
  if r != len(rows) - 1 || c != len(rows[r]) {
    t.Fatalf("cursor at end = %d,%d", r, c)
  }

  r, c = cursorAt(rows, len(rows[0]))
  if r != 1 || c != 0 {
    t.Fatalf("cursor at a row break = %d,%d", r, c)
  }

  got := layoutInput(nil, 10, 12)
  if len(got) != 1 || len(got[0]) != 0 {
    t.Fatalf("empty input = %q", got)
  }
}

func TestVisibleWidthSkipsStyles(t *testing.T) {
  n := visibleWidth("\x1b[1m\x1b[36m❯\x1b[0m ")
  if n != 2 {
    t.Fatalf("visibleWidth = %d", n)
  }
}

func TestFitLine(t *testing.T) {
  long := "waiting for the model · 20s (it may be loading or busy) · nothing from the model for 20s"
  got := fitLine(long, 40)
  if len([]rune(got)) != 40 || got[len(got) - len("…"):] != "…" {
    t.Fatalf("fitLine = %q (%d runes)", got, len([]rune(got)))
  }

  if fitLine("short", 40) != "short" || fitLine(long, 0) != long {
    t.Fatal("fitLine changed what already fits")
  }

  if fitLine("ç·ç·ç·", 4) != "ç·ç…" {
    t.Fatalf("multi-byte: %q", fitLine("ç·ç·ç·", 4))
  }
}
