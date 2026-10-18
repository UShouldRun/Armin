package sandbox

import (
  "context"
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"
)

func newTestConfig(t *testing.T, opts Options) *Config {
  t.Helper()
  root := t.TempDir()
  cfg, err := New(root, opts)
  if err != nil {
    t.Fatalf("New: %v", err)
  }
  return cfg
}

func TestNewAppliesDefaults(t *testing.T) {
  cfg := newTestConfig(t, Options{})
  if cfg.Timeout != 20 * time.Second {
    t.Errorf("default Timeout = %v, want 20s", cfg.Timeout)
  }
  if cfg.MaxOutput != 4000 {
    t.Errorf("default MaxOutput = %d, want 4000", cfg.MaxOutput)
  }
  if len(cfg.Allowlist) == 0 {
    t.Error("default Allowlist is empty, want DefaultAllowlist()")
  }
  if cfg.DryRun {
    t.Error("DryRun should default to false")
  }
}

func TestNewHonorsExplicitOptions(t *testing.T) {
  root := t.TempDir()
  cfg, err := New(root, Options{
    Timeout:   5 * time.Second,
    MaxOutput: 100,
    Allowlist: map[string]bool{ "echo": true },
    DryRun:    true,
  })
  if err != nil {
    t.Fatal(err)
  }
  if cfg.Timeout != 5 * time.Second || cfg.MaxOutput != 100 || !cfg.DryRun {
    t.Fatalf("New did not honor explicit options: %+v", cfg)
  }
  if !cfg.Allowlist["echo"] || len(cfg.Allowlist) != 1 {
    t.Fatalf("Allowlist = %v, want exactly {echo}", cfg.Allowlist)
  }
}

func TestNewResolvesRootToAbsolute(t *testing.T) {
  root := t.TempDir()
  wd, err := os.Getwd()
  if err != nil {
    t.Fatal(err)
  }
  rel, err := filepath.Rel(wd, root)
  if err != nil {
    t.Skip("temp dir not relative to cwd on this system")
  }

  cfg, err := New(rel, Options{})
  if err != nil {
    t.Fatal(err)
  }
  if !filepath.IsAbs(cfg.Root) {
    t.Fatalf("Root = %q, want an absolute path", cfg.Root)
  }
}

// --- ResolvePath: this is the actual security boundary, so it gets
// the most scrutiny. ---

func TestResolvePathWithinRoot(t *testing.T) {
  cfg := newTestConfig(t, Options{})
  err := os.WriteFile(filepath.Join(cfg.Root, "f.txt"), []byte("x"), 0o644)
  if err != nil {
    t.Fatal(err)
  }

  got, err := cfg.ResolvePath("f.txt")
  if err != nil {
    t.Fatalf("ResolvePath: %v", err)
  }
  want := filepath.Join(cfg.Root, "f.txt")
  if got != want {
    t.Fatalf("ResolvePath(f.txt) = %q, want %q", got, want)
  }
}

func TestResolvePathEmptyMeansRoot(t *testing.T) {
  cfg := newTestConfig(t, Options{})
  got, err := cfg.ResolvePath("")
  if err != nil {
    t.Fatal(err)
  }
  if got != cfg.Root {
    t.Fatalf("ResolvePath(\"\") = %q, want root %q", got, cfg.Root)
  }
}

func TestResolvePathRejectsDotDotEscape(t *testing.T) {
  cfg := newTestConfig(t, Options{})
  _, err := cfg.ResolvePath("../../etc/passwd")
  if err == nil {
    t.Fatal("ResolvePath allowed a ../ escape out of the project root")
  }
}

func TestResolvePathRejectsAbsoluteEscape(t *testing.T) {
  cfg := newTestConfig(t, Options{})
  _, err := cfg.ResolvePath("/etc/passwd")
  if err == nil {
    t.Fatal("ResolvePath allowed an absolute path outside the project root")
  }
}

func TestResolvePathAllowsAbsolutePathInsideRoot(t *testing.T) {
  cfg := newTestConfig(t, Options{})
  abs := filepath.Join(cfg.Root, "sub", "f.txt")
  err := os.MkdirAll(filepath.Dir(abs), 0o755)
  if err != nil {
    t.Fatal(err)
  }
  err = os.WriteFile(abs, []byte("x"), 0o644)
  if err != nil {
    t.Fatal(err)
  }

  got, err := cfg.ResolvePath(abs)
  if err != nil {
    t.Fatalf("ResolvePath(%q): %v", abs, err)
  }
  if got != abs {
    t.Fatalf("ResolvePath(%q) = %q", abs, got)
  }
}

func TestResolvePathRejectsSymlinkEscape(t *testing.T) {
  cfg := newTestConfig(t, Options{})

  outside := t.TempDir()
  secret := filepath.Join(outside, "secret.txt")
  err := os.WriteFile(secret, []byte("nope"), 0o644)
  if err != nil {
    t.Fatal(err)
  }

  link := filepath.Join(cfg.Root, "escape")
  err = os.Symlink(outside, link)
  if err != nil {
    t.Skipf("symlinks unsupported in this environment: %v", err)
  }

  _, err = cfg.ResolvePath("escape/secret.txt")
  if err == nil {
    t.Fatal("ResolvePath followed a symlink out of the project root without rejecting it")
  }
}

func TestResolvePathHandlesNonexistentPath(t *testing.T) {
  cfg := newTestConfig(t, Options{})

  // A nonexistent path still within root must resolve (write tools
  // are out of scope, but the jail check itself should stay honest —
  // see resolveExistingPrefix).
  got, err := cfg.ResolvePath("does/not/exist/yet.txt")
  if err != nil {
    t.Fatalf("ResolvePath on a nonexistent-but-in-root path: %v", err)
  }
  if !strings.HasPrefix(got, cfg.Root) {
    t.Fatalf("ResolvePath = %q, want a path under %q", got, cfg.Root)
  }
}

// --- CheckBinary ---

func TestCheckBinaryAllowsListed(t *testing.T) {
  cfg := newTestConfig(t, Options{ Allowlist: map[string]bool{ "git": true } })
  err := cfg.CheckBinary("git")
  if err != nil {
    t.Fatalf("CheckBinary(git): %v", err)
  }
}

func TestCheckBinaryRejectsUnlisted(t *testing.T) {
  cfg := newTestConfig(t, Options{ Allowlist: map[string]bool{ "git": true } })
  err := cfg.CheckBinary("rm")
  if err == nil {
    t.Fatal("CheckBinary allowed a binary not in the allowlist")
  }
}

func TestCheckBinaryRejectsPathQualified(t *testing.T) {
  cfg := newTestConfig(t, Options{ Allowlist: map[string]bool{ "git": true } })
  for _, bad := range []string{ "/usr/bin/git", "./git", "../git", "sub/git" } {
    err := cfg.CheckBinary(bad)
    if err == nil {
      t.Errorf("CheckBinary(%q) should reject a path-qualified binary name", bad)
    }
  }
}

// --- RunFlags ---

func TestRunFlags(t *testing.T) {
  var f RunFlags
  if f.Truncated() || f.DryRun() {
    t.Fatal("zero-value RunFlags should report both false")
  }

  f = RunTruncated
  if !f.Truncated() || f.DryRun() {
    t.Fatalf("RunTruncated flag = %+v", f)
  }

  f = RunTruncated | RunDryRun
  if !f.Truncated() || !f.DryRun() {
    t.Fatalf("combined flags = %+v, want both set", f)
  }
}

// --- Exec ---

func TestRunDryRunDoesNotExecute(t *testing.T) {
  cfg := newTestConfig(t, Options{ DryRun: true, Allowlist: map[string]bool{ "rm": true } })

  res, err := cfg.Exec(context.Background(), Command{ Binary: "rm", Args: []string{ "-rf", "/should/not/run" } })
  if err != nil {
    t.Fatalf("Run: %v", err)
  }
  if !res.Flags.DryRun() {
    t.Fatal("expected the DryRun flag to be set")
  }
  if !strings.Contains(res.Stdout, "rm -rf /should/not/run") {
    t.Fatalf("dry-run output = %q, want it to describe the command", res.Stdout)
  }
}

func TestRunForceCapturesOutputAndExitCode(t *testing.T) {
  cfg := newTestConfig(t, Options{})

  res, err := cfg.Exec(context.Background(), Command{ Binary: "sh", Args: []string{ "-c", "echo out; echo err >&2; exit 3" }, Mode: ExecForce })
  if err != nil {
    t.Fatalf("Exec: %v", err)
  }
  if strings.TrimSpace(res.Stdout) != "out" {
    t.Errorf("Stdout = %q, want \"out\"", res.Stdout)
  }
  if strings.TrimSpace(res.Stderr) != "err" {
    t.Errorf("Stderr = %q, want \"err\"", res.Stderr)
  }
  if res.ExitCode != 3 {
    t.Errorf("ExitCode = %d, want 3", res.ExitCode)
  }
}

func TestRunForceRunsInRoot(t *testing.T) {
  cfg := newTestConfig(t, Options{})

  res, err := cfg.Exec(context.Background(), Command{ Binary: "pwd", Args: nil, Mode: ExecForce })
  if err != nil {
    t.Fatal(err)
  }

  // Resolve both sides through EvalSymlinks: on macOS /tmp is itself a
  // symlink, and t.TempDir() can return either form depending on the
  // platform's temp dir.
  gotDir, _ := filepath.EvalSymlinks(strings.TrimSpace(res.Stdout))
  wantDir, _ := filepath.EvalSymlinks(cfg.Root)
  if gotDir != wantDir {
    t.Fatalf("pwd reported %q, want subprocess cwd to be %q", gotDir, wantDir)
  }
}

func TestRunForceTimesOut(t *testing.T) {
  cfg := newTestConfig(t, Options{ Timeout: 30 * time.Millisecond })

  _, err := cfg.Exec(context.Background(), Command{ Binary: "sleep", Args: []string{ "5" }, Mode: ExecForce })
  if err == nil || !strings.Contains(err.Error(), "timed out") {
    t.Fatalf("Exec error = %v, want a timeout error", err)
  }
}

func TestRunForceTruncatesLongOutput(t *testing.T) {
  cfg := newTestConfig(t, Options{ MaxOutput: 10 })

  res, err := cfg.Exec(context.Background(), Command{ Binary: "sh", Args: []string{ "-c", "printf '%050d' 0" }, Mode: ExecForce })
  if err != nil {
    t.Fatal(err)
  }
  if !res.Flags.Truncated() {
    t.Fatal("expected the Truncated flag to be set")
  }
  if !strings.Contains(res.Stdout, "truncated") {
    t.Fatalf("Stdout = %q, want a truncation notice", res.Stdout)
  }
}

func TestDescribeArgs(t *testing.T) {
  got := DescribeArgs("git", []string{ "log", "-3" })
  if got != "git log -3" {
    t.Fatalf("DescribeArgs = %q", got)
  }
}
