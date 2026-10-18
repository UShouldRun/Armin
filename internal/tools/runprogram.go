// RunProgram: a program the user approved (builds, scripts, the
// built binary), in the project, with an optional input and output
// file; Describe shows what's about to run.

package tools

import (
  "context"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "strings"

  "armin/internal/sandbox"
)

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
  // from dir, where the program runs. A shell's argument is a script,
  // not a path (shells are always asked, see sandbox.Privileged).
  paths := PathArgs(in.Binary, in.Args)
  if shell(in.Binary) {
    paths = nil
  }

  for _, p := range append(paths, nonEmpty(in.Stdin, in.Stdout)...) {
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

  var stdin io.Reader
  if in.Stdin != "" {
    f, err := os.Open(inDir(dir, in.Stdin))
    if err != nil {
      return nil, fmt.Errorf("\n\t... in RunProgram:\n\t stdin: %w", err)
    }

    defer f.Close()
    stdin = f
  }

  var stdout *os.File
  if in.Stdout != "" {
    stdout, err = os.Create(inDir(dir, in.Stdout))
    if err != nil {
      return nil, fmt.Errorf("\n\t... in RunProgram:\n\t stdout: %w", err)
    }

    defer stdout.Close()
  }

  cmd := sandbox.Command{
    Binary: bin,
    Args:   in.Args,
    Dir:    dir,
    Stdin:  stdin,
    Mode:   sandbox.ExecForce | sandbox.ExecFull | sandbox.ExecBuild,
  }

  // only a real file: a nil *os.File in an io.Writer isn't nil
  if stdout != nil {
    cmd.Stdout = stdout
  }

  res, err := cfg.Exec(ctx, cmd)

  if err != nil {
    return nil, fmt.Errorf("\n\t... in RunProgram:\n\t%w", err)
  }

  if stdout != nil {
    info, statErr := stdout.Stat()
    if statErr == nil {
      res.Stdout = fmt.Sprintf("(stdout written to %s, %d bytes)\n", in.Stdout, info.Size())
    }
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
    Command:   Describe(in),
    ExitCode:  res.ExitCode,
    Output:    tail,
    Truncated: truncated,
  }

  if dir != cfg.Root {
    out.Dir = strings.TrimPrefix(dir, cfg.Root + "/")
  }

  return out, nil
}

// Describe is the run as a shell line: "mpirun -np 1 ./fox < in > out".
func Describe(in RunProgramInput) string {
  s := strings.TrimSpace(sandbox.DescribeArgs(in.Binary, in.Args))
  if in.Stdin != "" {
    s += " < " + in.Stdin
  }

  if in.Stdout != "" {
    s += " > " + in.Stdout
  }

  return s
}

func shell(bin string) bool {
  base := filepath.Base(bin)
  return base == "bash" || base == "sh"
}

func nonEmpty(s ...string) []string {
  var out []string
  for _, v := range s {
    if v != "" {
      out = append(out, v)
    }
  }

  return out
}

// inDir is p relative to dir, where the program runs.
func inDir(dir, p string) string {
  if filepath.IsAbs(p) {
    return p
  }

  return filepath.Join(dir, p)
}
