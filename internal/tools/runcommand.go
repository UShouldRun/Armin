package tools

import (
  "context"
  "sort"

  "armin/internal/sandbox"
)

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

// RunCommand execs binary with args under the sandbox's allowlist,
// path jail (cwd is pinned to the project root by cfg.Run), timeout,
// and output truncation. When cfg.DryRun is set, nothing actually
// runs — cfg.Run reports what would have executed instead.
func RunCommand(
  ctx context.Context, cfg *sandbox.Config, in RunCommandInput,
) (*RunCommandOutput, *RunCommandError) {
  if in.Binary == "" {
    return nil, &RunCommandError{ Error: "binary is required" }
  }

  if err := cfg.CheckBinary(in.Binary); err != nil {
    return nil, &RunCommandError{
      Error:   err.Error(),
      Allowed: allowedBinaries(cfg),
    }
  }

  res, err := cfg.Run(ctx, in.Binary, in.Args)
  if err != nil {
    return nil, &RunCommandError{ Error: err.Error() }
  }

  return &RunCommandOutput{
    Stdout:    res.Stdout,
    Stderr:    res.Stderr,
    ExitCode:  res.ExitCode,
    Truncated: res.Flags.Truncated(),
    DryRun:    res.Flags.DryRun(),
  }, nil
}

func allowedBinaries(cfg *sandbox.Config) []string {
  allowed := make([]string, 0, len(cfg.Allowlist))
  for b := range cfg.Allowlist {
    allowed = append(allowed, b)
  }
  sort.Strings(allowed)
  return allowed
}
