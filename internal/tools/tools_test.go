package tools

import (
  "context"
  "os"
  "os/exec"
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
  err := os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    t.Fatal(err)
  }

  err = os.WriteFile(path, []byte(content), 0o644)
  if err != nil {
    t.Fatal(err)
  }
}

// --- read_file ---

func TestReadFileWholeSmallFile(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "one\ntwo\nthree")

  out, err := ReadFile(context.Background(), cfg, ReadFileInput{ Path: "a.txt" })
  if err != nil {
    t.Fatalf("ReadFile: %v", err)
  }

  if out.Content != "one\ntwo\nthree" || out.TotalLines != 3 {
    t.Fatalf("ReadFile = %+v", out)
  }
}

func TestReadFileCapsLongReads(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})

  // 300 lines of 100 bytes: under the line threshold, over the bytes
  line := strings.Repeat("x", 99) + "\n"
  writeFile(t, cfg.Root, "log.txt", strings.Repeat(line, 300))

  // 1-0 is "to the end": the read that once returned a whole log
  in := ReadFileInput{ Path: "log.txt", StartLine: 1 }
  out, err := ReadFile(context.Background(), cfg, in)
  if err != nil {
    t.Fatalf("ReadFile: %v", err)
  }

  if !out.Truncated || len(out.Content) > maxReadBytes ||
    out.NextLine != maxReadBytes / 100 + 1 {
    t.Fatalf("truncated %v, %d bytes, next_line %d",
      out.Truncated, len(out.Content), out.NextLine)
  }

  in.StartLine = out.NextLine
  out, err = ReadFile(context.Background(), cfg, in)
  if err != nil || out.Truncated || out.NextLine != 0 {
    t.Fatalf("resumed read: %+v, %v", out, err)
  }
}

func TestReadFileRequiresRangeAboveThreshold(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})

  var b strings.Builder
  for i := 0; i < maxWholeFileLines + 1; i++ {
    b.WriteString("line\n")
  }

  writeFile(t, cfg.Root, "big.txt", b.String())

  _, err := ReadFile(context.Background(), cfg, ReadFileInput{ Path: "big.txt" })
  if err == nil {
    t.Fatal("expected an error requiring start_line/end_line above the threshold")
  }

  out, err := ReadFile(context.Background(), cfg, ReadFileInput{ Path: "big.txt", StartLine: 1, EndLine: 2 })
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

  out, err := ReadFile(context.Background(), cfg, ReadFileInput{ Path: "a.txt", StartLine: 3, EndLine: 999 })
  if err != nil {
    t.Fatal(err)
  }

  if out.Content != "3\n4\n5" {
    t.Fatalf("clamped end_line: content = %q", out.Content)
  }

  _, err = ReadFile(context.Background(), cfg, ReadFileInput{ Path: "a.txt", StartLine: 10, EndLine: 20 })
  if err == nil {
    t.Fatal("expected an error for start_line beyond the end of the file")
  }

  _, err = ReadFile(context.Background(), cfg, ReadFileInput{ Path: "a.txt", StartLine: 4, EndLine: 2 })
  if err == nil {
    t.Fatal("expected an error for start_line after end_line")
  }
}

func TestReadFileRejectsDirectory(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  err := os.Mkdir(filepath.Join(cfg.Root, "sub"), 0o755)
  if err != nil {
    t.Fatal(err)
  }

  _, err = ReadFile(context.Background(), cfg, ReadFileInput{ Path: "sub" })
  if err == nil {
    t.Fatal("expected an error reading a directory as a file")
  }
}

func TestReadFileRejectsEscapingPath(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := ReadFile(context.Background(), cfg, ReadFileInput{ Path: "../outside.txt" })
  if err == nil {
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

  out, err := ListDirectory(context.Background(), cfg, ListDirectoryInput{ Depth: 1 })
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
  _, err := ListDirectory(context.Background(), cfg, ListDirectoryInput{ Path: "f.txt" })
  if err == nil {
    t.Fatal("expected an error listing a file as a directory")
  }
}

// --- run_command ---

func TestRunCommandRejectsUnlistedBinary(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{ Allowlist: map[string]bool{ "echo": true } })

  out, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "curl", Args: []string{ "example.com" } })
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
  _, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{})
  if cmdErr == nil {
    t.Fatal("expected an error when binary is empty")
  }
}

func TestRunCommandSuccess(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{ Allowlist: map[string]bool{ "echo": true } })

  out, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "echo", Args: []string{ "hi" } })
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
  cfg := newTestSandbox(t, sandbox.Options{ DryRun: true, Allowlist: map[string]bool{ "echo": true } })

  out, cmdErr := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "echo", Args: []string{ "hi" } })
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
  cfg, err := sandbox.New(t.TempDir(), sandbox.Options{ Allowlist: map[string]bool{ "rg": true } })
  if err != nil {
    t.Fatal(err)
  }

  _, err = cfg.Exec(context.Background(), sandbox.Command{ Binary: "rg", Args: []string{ "--version" }, Mode: sandbox.ExecForce })
  if err != nil {
    t.Skip("ripgrep (rg) not available — skipping search_code tests")
  }
}

func TestSearchCodeFindsMatches(t *testing.T) {
  requireRipgrep(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package main\n\nfunc NeedleFunc() {}\n")

  out, err := SearchCode(context.Background(), cfg, SearchCodeInput{ Pattern: "NeedleFunc" })
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

  out, err := SearchCode(context.Background(), cfg, SearchCodeInput{ Pattern: "DefinitelyNotPresent" })
  if err != nil {
    t.Fatalf("SearchCode: %v", err)
  }

  if out.Matches != "(no matches)" {
    t.Fatalf("Matches = %q, want the no-matches sentinel", out.Matches)
  }
}

func TestSearchCodeRequiresPattern(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := SearchCode(context.Background(), cfg, SearchCodeInput{})
  if err == nil {
    t.Fatal("expected an error for an empty pattern")
  }
}

// --- web_search ---

func TestWebSearchNilManagerIsCleanError(t *testing.T) {
  _, err := WebSearch(
    context.Background(), nil, WebSearchInput{ Query: "test" },
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
    _, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "sed", Args: args })
    if err == nil {
      t.Errorf("sed %v was allowed", args)
    }
  }

  out, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "sed", Args: []string{ "-n", "s/hello/bye/p", "a.txt" } })
  if err != nil || strings.TrimSpace(out.Stdout) != "bye" {
    t.Fatalf("read-only sed = %+v, %v", out, err)
  }

  // --sandbox blocks the w command, so sed can't write a file.
  RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "sed", Args: []string{ "w written.txt", "a.txt" } })
  _, statErr := os.Stat(filepath.Join(cfg.Root, "written.txt"))
  if statErr == nil {
    t.Fatal("sed's w command wrote a file")
  }

  for _, a := range []string{ "-delete", "-exec", "-fprint" } {
    _, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: "find", Args: []string{ ".", a } })
    if err == nil {
      t.Errorf("find %s was allowed", a)
    }
  }
}

func TestRunCommandJailsPathArgs(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "hello\n")
  ctx := context.Background()

  for _, args := range [][]string{
    { "../secret.txt" },
    { "/etc/hostname" },
    { "--file=/etc/hostname" },
    { "~/.bashrc" },
  } {
    _, err := RunCommand(ctx, cfg, RunCommandInput{ Binary: "cat", Args: args })
    if err == nil || !strings.Contains(err.Error, "request_access") {
      t.Errorf("cat %v = %v, want an outside-the-project error", args, err)
    }
  }

  out, err := RunCommand(ctx, cfg, RunCommandInput{ Binary: "grep", Args: []string{ "-n", "s/he/x/", "a.txt", "./a.txt" } })
  if err != nil || out.ExitCode > 1 {
    t.Fatalf("in-project paths and path-like patterns refused: %v %+v", err, out)
  }

  _, err = RunCommand(ctx, cfg, RunCommandInput{ Binary: "echo", Args: []string{ "/etc" } })
  if err != nil {
    t.Fatalf("echo takes text, not paths: %v", err)
  }

  _, err = RunCommand(ctx, cfg, RunCommandInput{ Binary: "cat", Args: []string{ "slides.PDF" } })
  if err == nil || !strings.Contains(err.Error, "read_pdf") {
    t.Fatalf("cat on a PDF = %v, want a read_pdf hint", err)
  }
}

func TestRunProgramPlanAndRun(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "hello.c", "#include <stdio.h>\nint main(void) { puts(\"hi\"); return 3; }\n")
  os.MkdirAll(filepath.Join(cfg.Root, "build"), 0o755)
  ctx := context.Background()

  for _, in := range []RunProgramInput{
    { Binary: "curl", Args: []string{ "example.com" } },
    { Binary: "rm", Args: []string{ "-rf", "/" } },
    { Binary: "rm", Args: []string{ "-rf", "~" } },
    { Binary: "/usr/bin/gcc" },
    { Binary: "gcc", Args: []string{ "-o", "../out", "hello.c" } },
    { Binary: "gcc", Dir: ".." },
    { Binary: "./missing" },
  } {
    _, _, err := PlanProgram(cfg, in)
    if err == nil {
      t.Errorf("PlanProgram(%+v) was allowed", in)
    }
  }

  _, err := exec.LookPath("cc")
  if err != nil {
    t.Skip("no C compiler")
  }

  out, err := RunProgram(ctx, cfg, RunProgramInput{ Binary: "cc", Args: []string{ "-o", "build/hello", "hello.c" } })
  if err != nil || out.ExitCode != 0 {
    t.Fatalf("compile = %+v, %v", out, err)
  }

  out, err = RunProgram(ctx, cfg, RunProgramInput{ Binary: "./hello", Dir: "build" })
  if err != nil || out.ExitCode != 3 || out.Dir != "build" {
    t.Fatalf("./hello in build = %+v, %v", out, err)
  }

  out, err = RunProgram(ctx, cfg, RunProgramInput{ Binary: "./build/hello" })
  if err != nil || out.ExitCode != 3 || !strings.Contains(out.Output, "hi") {
    t.Fatalf("run = %+v, %v", out, err)
  }
}

func TestRunCommandPointsAtRunProgram(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  for _, bin := range []string{ "make", "rm" } {
    _, err := RunCommand(context.Background(), cfg, RunCommandInput{ Binary: bin })
    if err == nil || !strings.Contains(err.Error, "run_program") {
      t.Errorf("run_command %s = %v, want a pointer to run_program", bin, err)
    }
  }
}
