// Inputs and outputs of runcommand.go's tools.

package tools

// RunCommandInput is the argument set for run_command. Args is always
// an array — never a single shell string — and the command execs
// directly (exec.Command(binary, args...)), never through sh -c. That
// closes the injection surface: there is no shell to interpret
// ; | & $() etc., they're just literal argv entries.
type RunCommandInput struct {
  Binary string   `json:"binary"`
  Args   []string `json:"args,omitempty"`
}

// Stdout/Stderr keep separate declarations, not grouped onto one line
// — Go's grouped-field syntax shares a single tag string across the
// whole group, and these two need distinct json tags.
type RunCommandOutput struct {
  Stdout    string `json:"stdout"`
  Stderr    string `json:"stderr"`
  ExitCode  int    `json:"exit_code"`
  Truncated bool   `json:"truncated,omitempty"`
  DryRun    bool   `json:"dry_run,omitempty"`
}

// RunCommandError is a structured error a small model can act on: it
// names what went wrong and, for allowlist failures, what IS
// allowed, so the model can self-correct next turn instead of
// retrying blind.
type RunCommandError struct {
  Error   string   `json:"error"`
  Allowed []string `json:"allowed,omitempty"`
}
