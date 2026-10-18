// Inputs and outputs of build.go's tools.

package tools

// Command is one project command. Args a caller passes replace
// DefaultArgs (e.g. "./..." for go test); Sep goes between Argv and
// caller args when there are any ("npm run test -- -t foo").
type Command struct {
  Argv, DefaultArgs []string
  Sep               string
  Source            string // where it came from, for the model
}

// ProjectCommands is the resolved test/build/lint trio for a root.
type ProjectCommands struct {
  Test, Build, Lint Command
}

// ProjectRunOutput is the shared result of run_tests/build_check/
// lint_check.
type ProjectRunOutput struct {
  Command   string `json:"command,omitempty"`
  Source    string `json:"source,omitempty"`
  Summary   string `json:"summary,omitempty"`
  Output    string `json:"output,omitempty"`
  Hint      string `json:"hint,omitempty"`
  Available bool   `json:"available"`
  Passed    bool   `json:"passed"`
  Truncated bool   `json:"truncated,omitempty"`
}

// ProjectRunInput is the argument set all three tools share.
// Package is the old Go-only argument, kept as a one-arg alias.
type ProjectRunInput struct {
  Args    []string `json:"args,omitempty"`
  Package string   `json:"package,omitempty"`
}

type (
  RunTestsInput   = ProjectRunInput
  BuildCheckInput = ProjectRunInput
  LintCheckInput  = ProjectRunInput
)
