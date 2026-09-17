package ui

import (
  "bufio"
  "context"
  "strings"
  "testing"
)

func rk(r rune) key       { return key{ r: r } }
func ck(c controlKey) key { return key{ control: c } }

func typeRunes(s *lineState, runes string) {
  for _, r := range runes {
    s.apply(rk(r))
  }
}

func TestLineStateTyping(t *testing.T) {
  var s lineState
  typeRunes(&s, "hi")
  if s.text() != "hi" {
    t.Fatalf("text = %q, want hi", s.text())
  }
}

func TestLineStateBackspace(t *testing.T) {
  var s lineState
  typeRunes(&s, "hi")
  out := s.apply(ck(keyBackspace))
  if !out.changed || s.text() != "h" {
    t.Fatalf("after backspace: text = %q, out = %+v", s.text(), out)
  }
}

func TestLineStateBackspaceOnEmptyIsNoOp(t *testing.T) {
  var s lineState
  out := s.apply(ck(keyBackspace))
  if out.changed {
    t.Fatal("backspace on an empty buffer should not report a change")
  }
}

func TestLineStateEnterSubmits(t *testing.T) {
  var s lineState
  typeRunes(&s, "hello")
  out := s.apply(ck(keyEnter))
  if !out.submit {
    t.Fatalf("out = %+v, want submit=true", out)
  }
  if s.text() != "hello" {
    t.Fatalf("text after submit = %q", s.text())
  }
}

func TestLineStateInterrupt(t *testing.T) {
  var s lineState
  typeRunes(&s, "partial")
  out := s.apply(ck(keyInterrupt))
  if !out.interrupted {
    t.Fatalf("out = %+v, want interrupted=true", out)
  }
}

func TestLineStateEOFOnlyWhenEmpty(t *testing.T) {
  var s lineState
  if out := s.apply(ck(keyEOF)); !out.eof {
    t.Fatalf("EOF on empty buffer: out = %+v, want eof=true", out)
  }

  var s2 lineState
  typeRunes(&s2, "x")
  if out := s2.apply(ck(keyEOF)); out.eof {
    t.Fatalf("EOF on non-empty buffer: out = %+v, want eof=false", out)
  }
}

func TestLineStateTabAcceptsSuggestion(t *testing.T) {
  var s lineState
  typeRunes(&s, "func ")
  s.setSuggestion("main() {}")

  out := s.apply(ck(keyTab))
  if !out.changed {
    t.Fatal("accepting a suggestion should report a change")
  }
  if s.text() != "func main() {}" {
    t.Fatalf("text after Tab-accept = %q", s.text())
  }
  if len(s.suggestion) != 0 {
    t.Fatal("suggestion should be cleared after being accepted")
  }
}

func TestLineStateTabWithNoSuggestionIsNoOp(t *testing.T) {
  var s lineState
  typeRunes(&s, "abc")
  out := s.apply(ck(keyTab))
  if out.changed {
    t.Fatal("Tab with no suggestion shown should not change the buffer")
  }
  if s.text() != "abc" {
    t.Fatalf("text = %q, want unchanged \"abc\"", s.text())
  }
}

func TestLineStateTypingClearsSuggestion(t *testing.T) {
  var s lineState
  typeRunes(&s, "fo")
  s.setSuggestion("o()")
  s.apply(rk('o')) // typing "o" instead of accepting the suggestion

  if len(s.suggestion) != 0 {
    t.Fatal("typing over a shown suggestion should clear it")
  }
  if s.text() != "foo" {
    t.Fatalf("text = %q, want foo", s.text())
  }
}

func TestLineStateBackspaceClearsSuggestion(t *testing.T) {
  var s lineState
  typeRunes(&s, "fo")
  s.setSuggestion("o()")
  s.apply(ck(keyBackspace))

  if len(s.suggestion) != 0 {
    t.Fatal("backspacing should clear a shown suggestion")
  }
}

func TestLineStateRender(t *testing.T) {
  var s lineState
  typeRunes(&s, "foo")
  s.setSuggestion("bar")

  visible, ghost := s.render("> ")
  if visible != "> foo" {
    t.Fatalf("visible = %q, want \"> foo\"", visible)
  }
  if ghost != "bar" {
    t.Fatalf("ghost = %q, want \"bar\"", ghost)
  }
}

func TestLineStateIgnoredKeyIsNoOp(t *testing.T) {
  var s lineState
  typeRunes(&s, "x")
  out := s.apply(ck(keyIgnored))
  if out.changed {
    t.Fatal("keyIgnored should never report a change")
  }
  if s.text() != "x" {
    t.Fatalf("text = %q, want unchanged \"x\"", s.text())
  }
}

// TestLineStateApplyAllocationsStayBounded: apply() runs on every
// single keystroke while the user types, so it needs to stay cheap
// regardless of how long the line already is — this is the
// "10-year-old laptop" check for the hot path in this package.
func TestLineStateApplyAllocationsStayBounded(t *testing.T) {
  var s lineState
  typeRunes(&s, "a fairly long line of typed text to build up a buffer")

  // Correctness first: a broken apply() that silently no-ops on
  // every keystroke would also allocate zero times and pass a
  // naive allocation check — assert the buffer actually grows and
  // shrinks before trusting the allocation count below.
  before := s.text()
  s.apply(rk('x'))
  if s.text() != before+"x" {
    t.Fatalf("typing didn't extend the buffer: got %q", s.text())
  }
  s.apply(ck(keyBackspace))
  if s.text() != before {
    t.Fatalf("backspace didn't shrink the buffer: got %q", s.text())
  }

  const allocLimit = 2 // append() may grow the backing array
  allocs := testing.AllocsPerRun(50, func() {
    s.apply(rk('x'))
    s.apply(ck(keyBackspace))
  })
  if allocs > allocLimit {
    t.Fatalf(
      "apply() allocated %.1f times per keystroke pair, want <= %d",
      allocs, allocLimit,
    )
  }
}

func TestReadLinePlain(t *testing.T) {
  reader := bufio.NewReader(strings.NewReader("hello world\nsecond\n"))

  line, err := readLinePlain(reader, "> ")
  if err != nil || line != "hello world" {
    t.Fatalf("readLinePlain #1 = (%q, %v)", line, err)
  }

  line, err = readLinePlain(reader, "> ")
  if err != nil || line != "second" {
    t.Fatalf("readLinePlain #2 = (%q, %v)", line, err)
  }
}

func TestReadLinePlainEOFWithNoTrailingNewline(t *testing.T) {
  reader := bufio.NewReader(strings.NewReader("no newline"))
  line, err := readLinePlain(reader, "> ")
  if err != nil || line != "no newline" {
    t.Fatalf("readLinePlain = (%q, %v), want (\"no newline\", nil)", line, err)
  }
}

func TestReadLinePlainTrueEOF(t *testing.T) {
  reader := bufio.NewReader(strings.NewReader(""))
  if _, err := readLinePlain(reader, "> "); err == nil {
    t.Fatal("expected an error reading from an already-empty stream")
  }
}

// TestReadLineWithGhostTextFallsBackWhenSuggestIsNil covers the path
// that actually runs in this environment (and in any non-interactive
// use of armin): ReadLineWithGhostText must behave exactly like
// readLinePlain when there's no suggest function, regardless of
// whether stdin happens to be a terminal — this is what makes
// /ghosttext safe to leave off by default.
func TestReadLineWithGhostTextFallsBackWhenSuggestIsNil(t *testing.T) {
  reader := bufio.NewReader(strings.NewReader("plain input\n"))

  line, err := ReadLineWithGhostText(
    context.Background(), reader, "> ", nil,
  )
  if err != nil || line != "plain input" {
    t.Fatalf("ReadLineWithGhostText(nil suggest) = (%q, %v)", line, err)
  }
}
