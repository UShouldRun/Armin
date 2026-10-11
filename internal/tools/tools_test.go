package tools

import (
  "context"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/sandbox"
  "armin/internal/searxng"
)

func newTestSandbox(t *testing.T, opts sandbox.Options) *sandbox.Config {
  t.Helper()
  root := t.TempDir()
  cfg, err := sandbox.New(root, opts)
  if err != nil {
    t.Fatalf("sandbox.New: %v", err)
  }
  return cfg
}

func writeFile(t *testing.T, root, name, content string) {
  t.Helper()
  path := filepath.Join(root, name)
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
    t.Fatal(err)
  }
}

// --- read_file ---

func TestReadFileWholeSmallFile(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "one\ntwo\nthree")

  out, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "a.txt"})
  if err != nil {
    t.Fatalf("ReadFile: %v", err)
  }
  if out.Content != "one\ntwo\nthree" || out.TotalLines != 3 {
    t.Fatalf("ReadFile = %+v", out)
  }
}

func TestReadFileRequiresRangeAboveThreshold(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})

  var b strings.Builder
  for i := 0; i < maxWholeFileLines + 1; i++ {
    b.WriteString("line\n")
  }
  writeFile(t, cfg.Root, "big.txt", b.String())

  if _, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "big.txt"}); err == nil {
    t.Fatal("expected an error requiring start_line/end_line above the threshold")
  }

  out, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "big.txt", StartLine: 1, EndLine: 2})
  if err != nil {
    t.Fatalf("ranged ReadFile: %v", err)
  }
  if out.Content != "line\nline" {
    t.Fatalf("ranged content = %q", out.Content)
  }
}

func TestReadFileRangeClampsAndValidates(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "1\n2\n3\n4\n5")

  out, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "a.txt", StartLine: 3, EndLine: 999})
  if err != nil {
    t.Fatal(err)
  }
  if out.Content != "3\n4\n5" {
    t.Fatalf("clamped end_line: content = %q", out.Content)
  }

  if _, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "a.txt", StartLine: 10, EndLine: 20}); err == nil {
    t.Fatal("expected an error for start_line beyond the end of the file")
  }
  if _, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "a.txt", StartLine: 4, EndLine: 2}); err == nil {
    t.Fatal("expected an error for start_line after end_line")
  }
}

func TestReadFileRejectsDirectory(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  if err := os.Mkdir(filepath.Join(cfg.Root, "sub"), 0o755); err != nil {
    t.Fatal(err)
  }
  if _, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "sub"}); err == nil {
    t.Fatal("expected an error reading a directory as a file")
  }
}

func TestReadFileRejectsEscapingPath(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  if _, err := ReadFile(context.Background(), cfg, ReadFileInput{Path: "../outside.txt"}); err == nil {
    t.Fatal("expected the sandbox to reject a path escaping the root")
  }
}

// --- list_directory ---

func TestListDirectorySkipsNoiseDirs(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "src/main.go", "package main")
  writeFile(t, cfg.Root, "node_modules/pkg/index.js", "")
  writeFile(t, cfg.Root, ".git/HEAD", "")

  out, err := ListDirectory(context.Background(), cfg, ListDirectoryInput{})
  if err != nil {
    t.Fatal(err)
  }
  if strings.Contains(out.Tree, "node_modules") {
    t.Fatalf("tree includes node_modules, should be skipped:\n%s", out.Tree)
  }
  if strings.Contains(out.Tree, ".git") {
    t.Fatalf("tree includes .git, should be skipped:\n%s", out.Tree)
  }
  if !strings.Contains(out.Tree, "src/") {
    t.Fatalf("tree missing src/:\n%s", out.Tree)
  }
}

func TestListDirectoryRespectsDepth(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a/b/c/deep.txt", "")

  out, err := ListDirectory(context.Background(), cfg, ListDirectoryInput{Depth: 1})
  if err != nil {
    t.Fatal(err)
  }
  if strings.Contains(out.Tree, "deep.txt") {
    t.Fatalf("depth=1 tree should not reach 3 levels down:\n%s", out.Tree)
  }
  if !strings.Contains(out.Tree, "a/") {
    t.Fatalf("depth=1 tree missing top-level a/:\n%s", out.Tree)
  }
}

func TestListDirectoryRejectsNonDirectory(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "f.txt", "x")
  if _, err := ListDirectory(context.Background(), cfg, ListDirectoryInput{Path: "f.txt"}); err == nil {
    t.Fatal("expected an error listing a file as a directory")
  }
}

// --- run_command ---

func TestRunCommandRejectsUnlistedBinary(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{Allowlist: map[string]bool{"echo": true}})

  out, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{Binary: "rm", Args: []string{"-rf", "/"}})
  if out != nil {
    t.Fatalf("RunCommand returned output for a disallowed binary: %+v", out)
  }
  if cmdErr == nil {
    t.Fatal("expected an error for a disallowed binary")
  }
  found := false
  for _, b := range cmdErr.Allowed {
    if b == "echo" {
      found = true
    }
  }
  if !found {
    t.Fatalf("error.Allowed = %v, want it to list \"echo\"", cmdErr.Allowed)
  }
}

func TestRunCommandRequiresBinary(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  if _, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{}); cmdErr == nil {
    t.Fatal("expected an error when binary is empty")
  }
}

func TestRunCommandSuccess(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{Allowlist: map[string]bool{"echo": true}})

  out, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{Binary: "echo", Args: []string{"hi"}})
  if cmdErr != nil {
    t.Fatalf("RunCommand: %+v", cmdErr)
  }
  if strings.TrimSpace(out.Stdout) != "hi" {
    t.Fatalf("Stdout = %q", out.Stdout)
  }
  if out.ExitCode != 0 || out.DryRun {
    t.Fatalf("unexpected result flags: %+v", out)
  }
}

func TestRunCommandDryRunFlowsThrough(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{DryRun: true, Allowlist: map[string]bool{"echo": true}})

  out, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{Binary: "echo", Args: []string{"hi"}})
  if cmdErr != nil {
    t.Fatalf("RunCommand: %+v", cmdErr)
  }
  if !out.DryRun {
    t.Fatalf("expected DryRun=true to flow through to the tool output, got %+v", out)
  }
}

// --- search_code ---
// SearchCode shells out to rg; skip if it's not on PATH rather than
// failing on machines without it installed.

func requireRipgrep(t *testing.T) {
  t.Helper()
  cfg, err := sandbox.New(t.TempDir(), sandbox.Options{Allowlist: map[string]bool{"rg": true}})
  if err != nil {
    t.Fatal(err)
  }
  if _, err := cfg.RunForce(context.Background(), "rg", []string{"--version"}); err != nil {
    t.Skip("ripgrep (rg) not available — skipping search_code tests")
  }
}

func TestSearchCodeFindsMatches(t *testing.T) {
  requireRipgrep(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package main\n\nfunc NeedleFunc() {}\n")

  out, err := SearchCode(context.Background(), cfg, SearchCodeInput{Pattern: "NeedleFunc"})
  if err != nil {
    t.Fatalf("SearchCode: %v", err)
  }
  if !strings.Contains(out.Matches, "NeedleFunc") {
    t.Fatalf("Matches = %q, want it to contain NeedleFunc", out.Matches)
  }
}

func TestSearchCodeNoMatches(t *testing.T) {
  requireRipgrep(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package main\n")

  out, err := SearchCode(context.Background(), cfg, SearchCodeInput{Pattern: "DefinitelyNotPresent"})
  if err != nil {
    t.Fatalf("SearchCode: %v", err)
  }
  if out.Matches != "(no matches)" {
    t.Fatalf("Matches = %q, want the no-matches sentinel", out.Matches)
  }
}

func TestSearchCodeRequiresPattern(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  if _, err := SearchCode(context.Background(), cfg, SearchCodeInput{}); err == nil {
    t.Fatal("expected an error for an empty pattern")
  }
}

// --- web_search ---

func TestWebSearchNilManagerIsCleanError(t *testing.T) {
  _, err := WebSearch(
    context.Background(), nil, WebSearchInput{Query: "test"},
  )
  if err == nil {
    t.Fatal("expected an error when no searxng.Manager is configured")
  }
  if !strings.Contains(err.Error(), "docker") {
    t.Fatalf("error = %q, want it to explain docker wasn't found", err)
  }
}

func TestWebSearchRequiresQuery(t *testing.T) {
  _, err := WebSearch(
    context.Background(), searxng.New(), WebSearchInput{},
  )
  if err == nil {
    t.Fatal("expected an error for an empty query")
  }
}

func TestRunCommandGuardsSedAndFind(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "hello\n")

  for _, args := range [][]string{
    { "-i", "s/hello/bye/", "a.txt" },
    { "-ni", "p", "a.txt" },
    { "-i.bak", "s/a/b/", "a.txt" },
    { "--in-place", "s/a/b/", "a.txt" },
  } {
    if _, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "sed", Args: args }); err == nil {
      t.Errorf("sed %v was allowed", args)
    }
  }

  out, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "sed", Args: []string{ "-n", "s/hello/bye/p", "a.txt" } })
  if err != nil || strings.TrimSpace(out.Stdout) != "bye" {
    t.Fatalf("read-only sed = %+v, %v", out, err)
  }

  // --sandbox blocks the w command, so sed can't write a file.
  RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "sed", Args: []string{ "w written.txt", "a.txt" } })
  if _, err := os.Stat(filepath.Join(cfg.Root, "written.txt")); err == nil {
    t.Fatal("sed's w command wrote a file")
  }

  for _, a := range []string{ "-delete", "-exec", "-fprint" } {
    if _, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "find", Args: []string{ ".", a } }); err == nil {
      t.Errorf("find %s was allowed", a)
    }
  }
}
