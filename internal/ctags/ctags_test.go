package ctags

import (
  "context"
  "os"
  "os/exec"
  "path/filepath"
  "testing"
)

func TestParseLine(t *testing.T) {
  cases := []struct {
    name string
    line string
    want Symbol
    ok   bool
  }{
    {
      name: "function with package scope",
      line: "New\tinternal/agent/loop.go\t31;\"\tf\tline:31\tpackage:agent\ttyperef:typename:*Loop",
      want: Symbol{Name: "New", Kind: "f", File: "internal/agent/loop.go", Line: 31, Scope: "package:agent"},
      ok:   true,
    },
    {
      name: "struct field with struct scope",
      line: "LLM\tinternal/agent/loop.go\t22;\"\tm\tline:22\tstruct:agent.Loop\ttyperef:typename:*llm.Client",
      want: Symbol{Name: "LLM", Kind: "m", File: "internal/agent/loop.go", Line: 22, Scope: "struct:agent.Loop"},
      ok:   true,
    },
    {
      name: "no scope field",
      line: "agent\tinternal/agent/loop.go\t4;\"\tp\tline:4",
      want: Symbol{Name: "agent", Kind: "p", File: "internal/agent/loop.go", Line: 4},
      ok:   true,
    },
    {
      name: "line number recovered from third column when line: field missing",
      line: "Foo\tfile.go\t7;\"\tf",
      want: Symbol{Name: "Foo", Kind: "f", File: "file.go", Line: 7},
      ok:   true,
    },
    {name: "empty line", line: "", ok: false},
    {name: "tag file pragma header", line: "!_TAG_FILE_FORMAT\t2\t/extended format/", ok: false},
    {name: "too few columns", line: "Foo\tfile.go", ok: false},
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      got, ok := parseLine(c.line)
      if ok != c.ok {
        t.Fatalf("parseLine(%q) ok = %v, want %v", c.line, ok, c.ok)
      }
      if !ok {
        return
      }
      if got != c.want {
        t.Fatalf("parseLine(%q) = %+v, want %+v", c.line, got, c.want)
      }
    })
  }
}

func TestRankSymbols(t *testing.T) {
  matches := []Symbol{
    {Name: "New", File: "other/pkg/file.go"},
    {Name: "New", File: "same/dir/other.go"},
    {Name: "New", File: "same/dir/target.go"},
    {Name: "New", File: "another/file.go"},
  }

  rankSymbols(matches, "same/dir/target.go")

  if matches[0].File != "same/dir/target.go" {
    t.Fatalf("expected exact-file match first, got %+v", matches[0])
  }
  if matches[1].File != "same/dir/other.go" {
    t.Fatalf("expected same-directory match second, got %+v", matches[1])
  }
  // The two lowest-priority matches keep their original relative order.
  if matches[2].File != "other/pkg/file.go" || matches[3].File != "another/file.go" {
    t.Fatalf("expected stable order for equal-weight matches, got %+v", matches[2:])
  }
}

func TestRankSymbolsNoFromFile(t *testing.T) {
  matches := []Symbol{{Name: "A", File: "a.go"}, {Name: "A", File: "b.go"}}
  original := append([]Symbol{}, matches...)

  rankSymbols(matches, "")

  for i := range matches {
    if matches[i] != original[i] {
      t.Fatalf("expected no reordering with empty fromFile, got %+v", matches)
    }
  }
}

func TestContainsFold(t *testing.T) {
  cases := []struct {
    s, substr string
    want      bool
  }{
    {"NewWidget", "widget", true},
    {"NewWidget", "WIDGET", true},
    {"NewWidget", "new", true},
    {"NewWidget", "gadget", false},
    {"NewWidget", "", true},
    {"", "x", false},
    {"", "", true},
    {"short", "muchlongerquery", false},
    {"ABC", "abc", true},
    {"abcabc", "cab", true},
  }

  for _, c := range cases {
    if got := containsFold(c.s, c.substr); got != c.want {
      t.Errorf("containsFold(%q, %q) = %v, want %v", c.s, c.substr, got, c.want)
    }
  }
}

func TestKindLabel(t *testing.T) {
  if got := KindLabel("f"); got != "function" {
    t.Fatalf("KindLabel(f) = %q, want function", got)
  }
  if got := KindLabel("z"); got != "z" {
    t.Fatalf("KindLabel(z) = %q, want fallback z", got)
  }
}

// findCtags locates a Universal Ctags binary for the integration test
// below, checking the same fallback location the LSP server does.
func findCtags(t *testing.T) string {
  t.Helper()
  if p, err := exec.LookPath("universal-ctags"); err == nil {
    return p
  }
  if home, err := os.UserHomeDir(); err == nil {
    p := filepath.Join(home, ".local", "bin", "universal-ctags")
    if _, err := os.Stat(p); err == nil {
      return p
    }
  }
  t.Skip("universal-ctags not found on PATH or in ~/.local/bin — skipping integration test")
  return ""
}

func TestIndexLookupAndSearch(t *testing.T) {
  binary := findCtags(t)

  dir := t.TempDir()
  src := "package widget\n\nfunc NewWidget() *Widget {\n\treturn &Widget{}\n}\n\ntype Widget struct {\n\tName string\n}\n"
  if err := os.WriteFile(filepath.Join(dir, "widget.go"), []byte(src), 0o644); err != nil {
    t.Fatal(err)
  }

  idx := New(dir, binary)
  ctx := context.Background()

  matches := idx.Lookup(ctx, "NewWidget", "")
  if len(matches) != 1 {
    t.Fatalf("Lookup(NewWidget) = %+v, want exactly 1 match", matches)
  }
  if matches[0].File != "widget.go" || matches[0].Line != 3 {
    t.Fatalf("Lookup(NewWidget) = %+v, want widget.go:3", matches[0])
  }

  if got := idx.Lookup(ctx, "DoesNotExist", ""); len(got) != 0 {
    t.Fatalf("Lookup(DoesNotExist) = %+v, want none", got)
  }

  found := idx.Search(ctx, "widget")
  if len(found) < 2 { // NewWidget + Widget, at minimum
    t.Fatalf("Search(widget) = %+v, want at least 2 matches", found)
  }

  if err := idx.LastError(); err != nil {
    t.Fatalf("LastError() = %v, want nil after a successful scan", err)
  }
}

func TestIndexRebuildOnMissingBinary(t *testing.T) {
  idx := New(t.TempDir(), "definitely-not-a-real-binary-armin-test")
  matches := idx.Lookup(context.Background(), "anything", "")

  if len(matches) != 0 {
    t.Fatalf("Lookup with a broken binary = %+v, want empty", matches)
  }
  if idx.LastError() == nil {
    t.Fatal("LastError() = nil, want a recorded scan failure")
  }
}
