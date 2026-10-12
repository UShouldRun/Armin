package lspserver

import (
  "fmt"
  "strings"
  "testing"

  "armin/internal/sandbox"
)

func TestFimWindow(t *testing.T) {
  text := "package main\n\nfunc main() {\n\tfmt.Println()\n}\n"

  w, ok := fimWindow(text, 3, 5)
  if !ok {
    t.Fatal("fimWindow returned ok = false")
  }

  if w.prefix != "package main\n\nfunc main() {\n" {
    t.Fatalf("prefix = %q", w.prefix)
  }

  if w.prompt != "\tfmt." || w.rest != "Println()" {
    t.Fatalf("prompt / rest = %q / %q", w.prompt, w.rest)
  }

  if w.suffix != "Println()\n}\n" || w.next != "}" {
    t.Fatalf("suffix / next = %q / %q", w.suffix, w.next)
  }

  if w.indent != 1 || w.head != "" {
    t.Fatalf("indent / head = %d / %q", w.indent, w.head)
  }

  if w.prefix + w.prompt + w.suffix != text {
    t.Fatal("prefix + prompt + suffix lost or duplicated bytes")
  }
}

// TestFimWindowPrefixIsAligned: moving down a line must not move the
// prefix's start, or llama-server's prompt cache misses every time.
func TestFimWindowPrefixIsAligned(t *testing.T) {
  var b strings.Builder
  for i := 0; i < 400; i++ {
    fmt.Fprintf(&b, "line %d\n", i)
  }

  text := b.String()

  a, _ := fimWindow(text, 200, 0)
  c, _ := fimWindow(text, 201, 0)
  if !strings.HasPrefix(c.prefix, a.prefix) {
    t.Fatal("prefix start moved between adjacent lines")
  }

  if !strings.HasPrefix(a.head, "line 0\n") {
    t.Fatalf("head = %q, want the file's first lines", a.head)
  }

  _, ok := fimWindow(text, 9999, 0)
  if ok {
    t.Fatal("expected ok = false past the end of the document")
  }

  w, _ := fimWindow("abc", 0, 99)
  if w.prompt != "abc" {
    t.Fatalf("character past the line end: prompt = %q", w.prompt)
  }

  w, _ = fimWindow("abc", 0, -5)
  if w.prompt != "" {
    t.Fatalf("negative character: prompt = %q", w.prompt)
  }
}

func TestFitToLine(t *testing.T) {
  cases := []struct {
    name, text, rest, next, want string
  }{
    { "no rest", "a, b)\n\tx()", "", "", "a, b)\n\tx()" },
    { "drops duplicated tail", "a, b)", ")", "", "a, b" },
    { "first line only with rest", "a)\nmore", ")", "", "a" },
    { "only the tail", ")", ")", "", "" },
    { "repeats next line", "\treturn nil", "", "return nil", "" },
    { "keeps leading space", " foo()", "", "", " foo()" },
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      got := fitToLine(c.text, c.rest, c.next)
      if got != c.want {
        t.Fatalf("fitToLine(%q) = %q, want %q", c.text, got, c.want)
      }
    })
  }
}

func TestOtherChunks(t *testing.T) {
  s := New(nil, &sandbox.Config{ Root: "/p" }, "", nil, nil, nil)
  s.setDoc("file:///p/a.go", "package a\n")
  s.setDoc("file:///p/a.go", "package a\n\nfunc A() {}\n")
  s.setDoc("file:///p/b.go", "package b\n")
  s.setDoc("file:///p/c.md", "# c\n")
  s.setDoc("file:///p/c.md", "# cc\n")

  got := s.otherChunks("file:///p/b.go")
  if len(got) != 1 || got[0].Filename != "a.go" {
    t.Fatalf("otherChunks = %+v, want just the edited a.go", got)
  }

  if !strings.Contains(got[0].Text, "func A()") {
    t.Fatalf("chunk = %q", got[0].Text)
  }
}

func TestFirstLines(t *testing.T) {
  text := "1\n2\n3\n4\n5"
  got := firstLines(text, 2)
  if got != "1\n2" {
    t.Fatalf("firstLines(_, 2) = %q, want \"1\\n2\"", got)
  }

  got = firstLines(text, 100)
  if got != text {
    t.Fatalf("firstLines with n > line count should return the input unchanged, got %q", got)
  }
}

func TestCleanCompletion(t *testing.T) {
  cases := []struct {
    name string
    raw  string
    want string
  }{
    { "plain code", "return x + 1", "return x + 1" },
    { "leading space kept", " x + 1\n", " x + 1" },
    {
      "fenced with language tag",
      "```go\nreturn x + 1\n```",
      "return x + 1",
    },
    { "trailing blank lines trimmed", "return x\n\n\n", "return x" },
    { "empty input", "", "" },
    { "whitespace only", "   \n  ", "" },
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      got := cleanCompletion(c.raw, 20)
      if got != c.want {
        t.Fatalf("cleanCompletion(%q) = %q, want %q", c.raw, got, c.want)
      }
    })
  }
}

func TestCleanCompletionCapsLineCount(t *testing.T) {
  raw := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10"
  got := cleanCompletion(raw, 3)
  if got != "1\n2\n3" {
    t.Fatalf("cleanCompletion with maxLines = 3 = %q, want \"1\\n2\\n3\"", got)
  }
}

// TestFimWindowAllocationsStayBounded: fimWindow runs on every
// completion request while you type, so it only slices.
func TestFimWindowAllocationsStayBounded(t *testing.T) {
  var b []byte
  for i := 0; i < 20000; i++ {
    b = append(b, []byte("some line of source code here\n")...)
  }

  big := string(b)

  allocs := testing.AllocsPerRun(20, func() {
    fimWindow(big, 10000, 10)
  })
  if allocs > 0 {
    t.Fatalf("fimWindow allocated %.0f times, want 0", allocs)
  }
}
