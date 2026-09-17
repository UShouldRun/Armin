package tools

import (
  "context"
  "os/exec"
  "strings"
  "testing"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

func requireCtags(t *testing.T) string {
  t.Helper()
  if p, err := exec.LookPath("universal-ctags"); err == nil {
    return p
  }
  t.Skip("universal-ctags not available — skipping real-index doc comment tests")
  return ""
}

// --- commentAbove / blockCommentAbove: pure logic, no ctags needed ---

func TestCommentAboveLineComments(t *testing.T) {
  lines := []string{
    "// Foo does a thing.",
    "// It's quite good at it.",
    "func Foo() {}",
  }
  got := commentAbove(lines, 3)
  want := "// Foo does a thing.\n// It's quite good at it."
  if got != want {
    t.Fatalf("commentAbove = %q, want %q", got, want)
  }
}

func TestCommentAboveBlockComment(t *testing.T) {
  lines := []string{
    "/* Foo does a thing.",
    " * Multi-line block. */",
    "func Foo() {}",
  }
  got := commentAbove(lines, 3)
  want := "/* Foo does a thing.\n * Multi-line block. */"
  if got != want {
    t.Fatalf("commentAbove = %q, want %q", got, want)
  }
}

func TestCommentAboveNoneWhenBlankLineBreaksIt(t *testing.T) {
  lines := []string{
    "// unrelated comment",
    "",
    "func Foo() {}",
  }
  got := commentAbove(lines, 3)
  if got != "" {
    t.Fatalf("commentAbove = %q, want empty (blank line should break the run)", got)
  }
}

func TestCommentAboveNoneAtFileStart(t *testing.T) {
  lines := []string{ "func Foo() {}" }
  if got := commentAbove(lines, 1); got != "" {
    t.Fatalf("commentAbove = %q, want empty", got)
  }
}

// --- read_doc_comment: nil index ---

func TestReadDocCommentNilIndexIsCleanError(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  in := ReadDocCommentInput{Name: "Foo"}
  if _, err := ReadDocComment(context.Background(), nil, cfg, in); err == nil {
    t.Fatal("expected an error for a nil ctags index")
  }
}

func TestReadDocCommentRequiresName(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  idx := ctags.New(cfg.Root, "")
  if _, err := ReadDocComment(context.Background(), idx, cfg, ReadDocCommentInput{}); err == nil {
    t.Fatal("expected an error for an empty name")
  }
}

// --- read_doc_comment: real ctags index end-to-end ---

func TestReadDocCommentEndToEnd(t *testing.T) {
  ctagsBin := requireCtags(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go",
    "package main\n\n// Greet says hello to name.\nfunc Greet(name string) string {\n  return \"hello \" + name\n}\n",
  )

  idx := ctags.New(cfg.Root, ctagsBin)
  out, err := ReadDocComment(context.Background(), idx, cfg, ReadDocCommentInput{Name: "Greet"})
  if err != nil {
    t.Fatalf("ReadDocComment: %v", err)
  }
  if !strings.Contains(out.Comment, "Greet says hello") {
    t.Fatalf("Comment = %q, want it to contain the doc comment", out.Comment)
  }
}

func TestReadDocCommentUnknownSymbol(t *testing.T) {
  ctagsBin := requireCtags(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package main\n\nfunc Foo() {}\n")

  idx := ctags.New(cfg.Root, ctagsBin)
  in := ReadDocCommentInput{Name: "DefinitelyNotDefinedAnywhere"}
  if _, err := ReadDocComment(context.Background(), idx, cfg, in); err == nil {
    t.Fatal("expected an error for an undefined symbol")
  }
}
