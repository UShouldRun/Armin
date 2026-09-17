package tools

import (
  "context"
  "fmt"
  "os/exec"
  "strings"

  "armin/internal/sandbox"
)

// summarizeGoOutput keeps only go test/build's own "ok"/"FAIL"/
// "---"/"#" result lines — the signal in otherwise-verbose output.
func summarizeGoOutput(output string) string {
  var lines []string
  for _, line := range strings.Split(output, "\n") {
    trimmed := strings.TrimSpace(line)
    isResultLine := strings.HasPrefix(trimmed, "ok ") ||
      strings.HasPrefix(trimmed, "FAIL") ||
      strings.HasPrefix(trimmed, "---") ||
      strings.HasPrefix(trimmed, "# ")
    if isResultLine {
      lines = append(lines, trimmed)
    }
  }
  if len(lines) == 0 {
    return "(no per-package result lines found)"
  }
  return strings.Join(lines, "\n")
}

// --- run_tests ---

type RunTestsInput struct {
  Package string `json:"package,omitempty"` // default: ./...
}

type RunTestsOutput struct {
  Passed    bool   `json:"passed"`
  Summary   string `json:"summary"` // per-package ok/FAIL lines only
  Output    string `json:"output"`  // full go test output
  Truncated bool   `json:"truncated"`
}

// RunTests runs `go test` against Package (default every package)
// and reports pass/fail plus a summary — one call instead of the
// model invoking run_command with the right flags itself and
// re-deriving pass/fail from raw output.
func RunTests(
  ctx context.Context, cfg *sandbox.Config, in RunTestsInput,
) (*RunTestsOutput, error) {
  pkg := in.Package
  if pkg == "" {
    pkg = "./..."
  }

  res, err := cfg.RunForce(ctx, "go", []string{ "test", pkg })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in RunTests:\n\t%w", err)
  }

  output := res.Stdout + res.Stderr
  return &RunTestsOutput{
    Passed: res.ExitCode == 0, Summary: summarizeGoOutput(output),
    Output: output, Truncated: res.Flags.Truncated(),
  }, nil
}

// --- build_check ---

type BuildCheckInput struct {
  Package string `json:"package,omitempty"` // default: ./...
  Vet     bool   `json:"vet,omitempty"`     // run "go vet" instead of "go build"
}

type BuildCheckOutput struct {
  Passed    bool   `json:"passed"`
  Output    string `json:"output"`
  Truncated bool   `json:"truncated"`
}

// BuildCheck runs `go build` (or, with Vet, `go vet`) against
// Package and reports pass/fail plus any errors.
func BuildCheck(
  ctx context.Context, cfg *sandbox.Config, in BuildCheckInput,
) (*BuildCheckOutput, error) {
  pkg := in.Package
  if pkg == "" {
    pkg = "./..."
  }

  subcmd := "build"
  if in.Vet {
    subcmd = "vet"
  }
  res, err := cfg.RunForce(ctx, "go", []string{ subcmd, pkg })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in BuildCheck:\n\t%w", err)
  }

  output := res.Stdout + res.Stderr
  if output == "" && res.ExitCode == 0 {
    output = "(no errors)"
  }
  return &BuildCheckOutput{
    Passed: res.ExitCode == 0, Output: output, Truncated: res.Flags.Truncated(),
  }, nil
}

// --- lint_check ---

type LintCheckInput struct {
  Package string `json:"package,omitempty"`
}

type LintCheckOutput struct {
  Available bool   `json:"available"`
  Passed    bool   `json:"passed,omitempty"`
  Output    string `json:"output,omitempty"`
  Truncated bool   `json:"truncated,omitempty"`
}

// LintCheck runs golangci-lint if it's on PATH; if not, it reports
// Available: false rather than an error — the same degrade-
// gracefully pattern -ctags-bin and web_search's docker check use.
// golangci-lint is never added to sandbox.DefaultAllowlist: this is
// a fixed, trusted invocation this tool itself controls, the same
// way search_code controls its own ripgrep call — not the arbitrary
// escape hatch run_command's allowlist guards.
func LintCheck(
  ctx context.Context, cfg *sandbox.Config, in LintCheckInput,
) (*LintCheckOutput, error) {
  if _, err := exec.LookPath("golangci-lint"); err != nil {
    return &LintCheckOutput{ Available: false }, nil
  }

  pkg := in.Package
  if pkg == "" {
    pkg = "./..."
  }

  res, err := cfg.RunForce(ctx, "golangci-lint", []string{ "run", pkg })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in LintCheck:\n\t%w", err)
  }

  output := res.Stdout + res.Stderr
  if output == "" && res.ExitCode == 0 {
    output = "(no issues)"
  }
  return &LintCheckOutput{
    Available: true, Passed: res.ExitCode == 0,
    Output: output, Truncated: res.Flags.Truncated(),
  }, nil
}
