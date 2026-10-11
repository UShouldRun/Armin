package tools

import (
  "context"
  "fmt"
  "sort"
  "strings"

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

  args, err := guardArgs(in.Binary, in.Args)
  if err != nil {
    return nil, &RunCommandError{ Error: err.Error() }
  }

  res, err := cfg.Run(ctx, in.Binary, args)
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

// findWrites are find's actions that delete, run or write.
var findWrites = map[string]bool{
  "-delete": true, "-exec": true, "-execdir": true, "-ok": true,
  "-okdir": true, "-fprint": true, "-fprint0": true, "-fprintf": true,
  "-fls": true,
}

// sedInPlace spots -i in any spelling: -i, -i.bak, -ni (short flags
// bundled), --in-place[=suffix]. sed's other short flags (-n -E -r
// -s -u -z) have no "i", and -e takes its script as the next word.
func sedInPlace(a string) bool {
  if a == "--in-place" || strings.HasPrefix(a, "--in-place=") {
    return true
  }

  short := strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--")

  return short && !strings.HasPrefix(a, "-e") && strings.ContainsRune(a, 'i')
}

// guardArgs keeps the allowlisted tools that *can* write read-only:
// sed never edits in place and always runs in --sandbox (which
// rejects its w, r and e commands); find can't delete, exec or write.
func guardArgs(binary string, args []string) ([]string, error) {
  switch binary {
  case "sed":
    for _, a := range args {
      if sedInPlace(a) {
        return nil, fmt.Errorf(
          "sed -i (editing in place) isn't allowed: run_command is " +
            "read-only — use edit_file to change files",
        )
      }
    }

    return append([]string{ "--sandbox" }, args...), nil

  case "find":
    for _, a := range args {
      if findWrites[a] {
        return nil, fmt.Errorf(
          "find %s isn't allowed: run_command is read-only", a,
        )
      }
    }
  }

  return args, nil
}

func allowedBinaries(cfg *sandbox.Config) []string {
  allowed := make([]string, 0, len(cfg.Allowlist))
  for b := range cfg.Allowlist {
    allowed = append(allowed, b)
  }

  sort.Strings(allowed)

  return allowed
}
