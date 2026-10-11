package tools

import (
  "context"
  "os/exec"
  "strings"
  "testing"

  "armin/internal/sandbox"
)

func requireGo(t *testing.T) {
  t.Helper()
  if _, err := exec.LookPath("go"); err != nil {
    t.Skip("go toolchain not available — skipping go-backed tool tests")
  }
}

// writePassingModule writes a minimal, dependency-free Go module
// with one passing test — enough for `go test`/`go build`/`go vet`
// to have something real to run against.
func writePassingModule(t *testing.T, root string) {
  t.Helper()
  writeFile(t, root, "go.mod", "module fixture\n\ngo 1.21\n")
  writeFile(t, root, "fixture.go", "package fixture\n\nfunc Add(a, b int) int { return a + b }\n")
  writeFile(t, root, "fixture_test.go",
    "package fixture\n\nimport \"testing\"\n\n"+
      "func TestAdd(t *testing.T) {\n  if Add(2, 3) != 5 {\n    t.Fatal(\"bad\")\n  }\n}\n",
  )
}

func writeFailingModule(t *testing.T, root string) {
  t.Helper()
  writeFile(t, root, "go.mod", "module fixture\n\ngo 1.21\n")
  writeFile(t, root, "fixture.go", "package fixture\n\nfunc Add(a, b int) int { return a + b }\n")
  writeFile(t, root, "fixture_test.go",
    "package fixture\n\nimport \"testing\"\n\n"+
      "func TestAdd(t *testing.T) {\n  if Add(2, 3) != 999 {\n    t.Fatal(\"bad\")\n  }\n}\n",
  )
}

// --- run_tests ---

func TestRunTestsPassing(t *testing.T) {
  requireGo(t)
  cfg := newTestSandbox(t, sandbox.Options{ Timeout: 60_000_000_000 })
  writePassingModule(t, cfg.Root)

  out, err := RunTests(context.Background(), cfg, RunTestsInput{})
  if err != nil {
    t.Fatalf("RunTests: %v", err)
  }
  if !out.Passed {
    t.Fatalf("Passed = false, want true — output: %s", out.Output)
  }
}

func TestRunTestsFailing(t *testing.T) {
  requireGo(t)
  cfg := newTestSandbox(t, sandbox.Options{ Timeout: 60_000_000_000 })
  writeFailingModule(t, cfg.Root)

  out, err := RunTests(context.Background(), cfg, RunTestsInput{})
  if err != nil {
    t.Fatalf("RunTests: %v", err)
  }
  if out.Passed {
    t.Fatal("Passed = true, want false for a failing test")
  }
  if out.Summary == "(no per-package result lines found)" {
    t.Fatal("expected a FAIL summary line")
  }
}

// --- build_check ---

func TestBuildCheckPassing(t *testing.T) {
  requireGo(t)
  cfg := newTestSandbox(t, sandbox.Options{ Timeout: 60_000_000_000 })
  writePassingModule(t, cfg.Root)

  out, err := BuildCheck(context.Background(), cfg, BuildCheckInput{})
  if err != nil {
    t.Fatalf("BuildCheck: %v", err)
  }
  if !out.Passed {
    t.Fatalf("Passed = false, want true — output: %s", out.Output)
  }
}

func TestBuildCheckFailingReportsTail(t *testing.T) {
  requireGo(t)
  cfg := newTestSandbox(t, sandbox.Options{ Timeout: 60_000_000_000 })
  writeFile(t, cfg.Root, "go.mod", "module fixture\n\ngo 1.21\n")
  writeFile(t, cfg.Root, "bad.go", "package fixture\n\nfunc F() int { return \"x\" }\n")

  out, err := BuildCheck(context.Background(), cfg, BuildCheckInput{})
  if err != nil {
    t.Fatalf("BuildCheck: %v", err)
  }
  if out.Passed || out.Command != "go build ./..." {
    t.Fatalf("out = %+v, want a failing go build ./...", out)
  }
}

// --- lint_check ---

func TestLintCheckUnavailableIsNotAnError(t *testing.T) {
  if _, err := exec.LookPath("golangci-lint"); err == nil {
    t.Skip("golangci-lint is installed — this test only covers the absent case")
  }
  cfg := newTestSandbox(t, sandbox.Options{})

  out, err := LintCheck(context.Background(), cfg, LintCheckInput{})
  if err != nil {
    t.Fatalf("LintCheck: %v", err)
  }
  if out.Available {
    t.Fatal("Available = true, but golangci-lint isn't installed in this environment")
  }
}

// --- detection ---

func TestDetectCommandsPrecedence(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "go.mod", "module fixture\n")
  writeFile(t, cfg.Root, "Makefile", "build:\n\tgo build\nVAR := x\n")
  writeFile(t, cfg.Root, ".armin/commands.conf", "test_cmd = go test -race ./...\n")
  cfg.Commands.Lint = []string{ "mylint", "--strict" }

  pc := DetectCommands(cfg)
  if got := strings.Join(pc.Test.argv(nil), " "); got != "go test -race ./..." {
    t.Errorf("test = %q, want the project file's command", got)
  }
  if got := strings.Join(pc.Build.argv(nil), " "); got != "make build" {
    t.Errorf("build = %q, want the Makefile target", got)
  }
  if got := strings.Join(pc.Lint.argv(nil), " "); got != "mylint --strict" {
    t.Errorf("lint = %q, want the user's config", got)
  }
  if got := strings.Join(pc.Test.argv([]string{ "./x" }), " "); got != "go test -race ./... ./x" {
    t.Errorf("test with args = %q", got)
  }
}

func TestDetectCommandsNode(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "package.json", `{"scripts":{"test":"vitest","lint":"eslint ."}}`)
  writeFile(t, cfg.Root, "tsconfig.json", "{}")

  pc := DetectCommands(cfg)
  if got := strings.Join(pc.Test.argv([]string{ "-t", "foo" }), " "); got != "npm run test -- -t foo" {
    t.Errorf("test = %q", got)
  }
  if got := strings.Join(pc.Build.argv(nil), " "); got != "npx tsc --noEmit" {
    t.Errorf("build = %q, want the tsconfig fallback", got)
  }
}

func TestRunProjectWithNoCommandIsAHint(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  out, err := RunTests(context.Background(), cfg, RunTestsInput{})
  if err != nil {
    t.Fatal(err)
  }
  if out.Available || out.Hint == "" {
    t.Fatalf("out = %+v, want available:false with a hint", out)
  }
}
