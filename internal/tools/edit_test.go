package tools

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"

  "armin/internal/sandbox"
)

func readBack(t *testing.T, root, name string) string {
  t.Helper()
  b, err := os.ReadFile(filepath.Join(root, name))
  if err != nil {
    t.Fatal(err)
  }

  return string(b)
}

func TestWriteFileCreatesWithParents(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  tr := NewFileTracker()

  out, err := WriteFile(cfg, tr, WriteFileInput{ Path: "a/b/c.txt", Content: "hi\n" })
  if err != nil {
    t.Fatalf("WriteFile: %v", err)
  }

  if !out.Created || out.LinesAdded != 1 {
    t.Fatalf("out = %+v", out)
  }

  if got := readBack(t, cfg.Root, "a/b/c.txt"); got != "hi\n" {
    t.Fatalf("content = %q", got)
  }
}

func TestWriteFileRefusesUnreadExistingFile(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "x.txt", "old")
  tr := NewFileTracker()

  if _, err := WriteFile(cfg, tr, WriteFileInput{ Path: "x.txt", Content: "new" }); err == nil {
    t.Fatal("expected an error overwriting a file that was never read")
  }

  tr.Mark(filepath.Join(cfg.Root, "x.txt"))
  if _, err := WriteFile(cfg, tr, WriteFileInput{ Path: "x.txt", Content: "new" }); err != nil {
    t.Fatalf("WriteFile after read: %v", err)
  }
}

func TestEditFileRefusesStaleRead(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "x.txt", "alpha\n")
  path := filepath.Join(cfg.Root, "x.txt")
  tr := NewFileTracker()
  tr.Mark(path)

  later := time.Now().Add(time.Hour)
  os.WriteFile(path, []byte("beta\n"), 0o644)
  os.Chtimes(path, later, later)

  _, err := EditFile(cfg, tr, EditFileInput{ Path: "x.txt", OldString: "beta", NewString: "gamma" })
  if err == nil || !strings.Contains(err.Error(), "changed on disk") {
    t.Fatalf("err = %v, want a stale-read error", err)
  }
}

func TestEditFileExactUniqueReplace(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "f.go", "a := 1\nb := 2\na := 1\n")
  tr := NewFileTracker()
  tr.Mark(filepath.Join(cfg.Root, "f.go"))

  _, err := EditFile(cfg, tr, EditFileInput{ Path: "f.go", OldString: "a := 1", NewString: "a := 9" })
  if err == nil || !strings.Contains(err.Error(), "matches 2 times") {
    t.Fatalf("err = %v, want an ambiguity error", err)
  }

  out, err := EditFile(cfg, tr, EditFileInput{
    Path: "f.go", OldString: "a := 1", NewString: "a := 9", ReplaceAll: true,
  })
  if err != nil {
    t.Fatalf("EditFile replace_all: %v", err)
  }

  if out.Replacements != 2 {
    t.Fatalf("Replacements = %d, want 2", out.Replacements)
  }

  out, err = EditFile(cfg, tr, EditFileInput{ Path: "f.go", OldString: "b := 2\n", NewString: "" })
  if err != nil {
    t.Fatalf("EditFile delete: %v", err)
  }

  if got := readBack(t, cfg.Root, "f.go"); got != "a := 9\na := 9\n" {
    t.Fatalf("content = %q", got)
  }

  if out.LinesRemoved != 1 {
    t.Fatalf("LinesRemoved = %d, want 1", out.LinesRemoved)
  }
}

func TestEditFileNotFoundPointsAtNearMiss(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "f.go", "func A() {\n    return\n}\n")
  tr := NewFileTracker()
  tr.Mark(filepath.Join(cfg.Root, "f.go"))

  _, err := EditFile(cfg, tr, EditFileInput{
    Path: "f.go", OldString: "\treturn\n}", NewString: "\treturn 1\n}",
  })
  if err == nil || !strings.Contains(err.Error(), "line 2") {
    t.Fatalf("err = %v, want a pointer at line 2", err)
  }
}

func TestWriteRefusesGitDirAndEscape(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  for _, p := range []string{ ".git/config", "../outside.txt", "." } {
    if _, err := WriteFile(cfg, nil, WriteFileInput{ Path: p, Content: "x" }); err == nil {
      t.Errorf("WriteFile(%q) succeeded, want refusal", p)
    }
  }
}

func TestDryRunWritesNothing(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{ DryRun: true })
  out, err := WriteFile(cfg, nil, WriteFileInput{ Path: "n.txt", Content: "x" })
  if err != nil || !out.DryRun {
    t.Fatalf("out = %+v, err = %v", out, err)
  }

  if _, err := os.Stat(filepath.Join(cfg.Root, "n.txt")); !os.IsNotExist(err) {
    t.Fatal("dry run wrote the file")
  }
}

func TestPreviewShowsChangedRegion(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "f.txt", "1\n2\n3\n4\n5\n6\n")
  c, err := PlanEdit(cfg, nil, EditFileInput{ Path: "f.txt", OldString: "4\n", NewString: "four\n" })
  if err != nil {
    t.Fatal(err)
  }

  want := "f.txt @ line 4\n  2\n  3\n- 4\n+ four\n  5\n  6\n"
  if got := c.Preview(); got != want {
    t.Fatalf("Preview =\n%s\nwant\n%s", got, want)
  }
}
