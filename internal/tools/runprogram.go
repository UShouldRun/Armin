package tools

import (
  "context"
  "fmt"
  "path/filepath"
  "strings"

  "armin/internal/sandbox"
)

// RunProgramInput is the argument set for run_program.
type RunProgramInput struct {
  Binary string   `json:"binary"`
  Args   []string `json:"args,omitempty"`
  Dir    string   `json:"dir,omitempty"` // inside the project; default its root
}

// RunProgramOutput keeps the tail of the output, where compilers and
// test runners put the errors and the verdict.
type RunProgramOutput struct {
  Command   string `json:"command"`
  Dir       string `json:"dir,omitempty"`
  ExitCode  int    `json:"exit_code"`
  Output    string `json:"output"`
  Truncated bool   `json:"truncated,omitempty"`
}

// PlanProgram checks a run_program call without running it, and
// returns the working directory and the program to exec. The caller
// asks the user between PlanProgram and RunProgram.
func PlanProgram(
  cfg *sandbox.Config, in RunProgramInput,
) (dir, bin string, err error) {
  if in.Binary == "" {
    return "", "", fmt.Errorf("binary is required")
  }

  dir, err = cfg.ResolvePath(in.Dir)
  if err != nil || !cfg.InRoot(dir) {
    return "", "", fmt.Errorf("dir %q must be inside the project", in.Dir)
  }

  bin, err = cfg.CheckExec(in.Binary, dir)
  if err != nil {
    return "", "", err
  }

  // These write and run code, so paths stay in the project proper,
  // not in directories granted for reading. Relative ones are taken
  // from dir, where the program runs.
  for _, p := range PathArgs(in.Binary, in.Args) {
    if !filepath.IsAbs(p) {
      p = filepath.Join(dir, p)
    }

    resolved, err := cfg.ResolvePath(p)
    if err != nil || !cfg.InRoot(resolved) {
      return "", "", fmt.Errorf(
        "%q is outside the project: run_program only works inside it", p,
      )
    }
  }

  return dir, bin, nil
}

// RunProgram runs a planned program under the build timeout, with no
// shell, and returns its combined output's tail.
func RunProgram(
  ctx context.Context, cfg *sandbox.Config, in RunProgramInput,
) (*RunProgramOutput, error) {
  dir, bin, err := PlanProgram(cfg, in)
  if err != nil {
    return nil, err
  }

  if cfg.DryRun {
    return &RunProgramOutput{
      Command: "[dry-run] would execute: " + sandbox.DescribeArgs(in.Binary, in.Args),
    }, nil
  }

  res, err := cfg.RunIn(ctx, dir, bin, in.Args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in RunProgram:\n\t%w", err)
  }

  output := res.Stdout
  if res.Stderr != "" {
    if output != "" && !strings.HasSuffix(output, "\n") {
      output += "\n"
    }

    output += res.Stderr
  }

  tail, truncated := sandbox.TruncateTail(output, cfg.MaxOutput)
  out := &RunProgramOutput{
    Command:   strings.TrimSpace(sandbox.DescribeArgs(in.Binary, in.Args)),
    ExitCode:  res.ExitCode,
    Output:    tail,
    Truncated: truncated,
  }

  if dir != cfg.Root {
    out.Dir = strings.TrimPrefix(dir, cfg.Root + "/")
  }

  return out, nil
}
