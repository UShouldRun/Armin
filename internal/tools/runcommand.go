package tools

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "armin/internal/sandbox"
)

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

  err := cfg.CheckBinary(in.Binary)
  if err != nil {
    // A program run_program has: say so, rather than listing what
    // run_command allows.
    if in.Binary == "bash" || in.Binary == "sh" {
      return nil, &RunCommandError{
        Error: "run_command has no shell: for pipes and redirects call " +
          "run_program with binary " + in.Binary + " and the same args " +
          "(the user approves it), or use run_program's stdin/stdout " +
          "for a plain < file / > file",
      }
    }

    if cfg.ExecAllowlist[in.Binary] {
      return nil, &RunCommandError{
        Error: in.Binary + " isn't read-only, so run_command can't run it: " +
          "call run_program with the same binary and args (the user approves it)",
      }
    }

    return nil, &RunCommandError{
      Error:   err.Error(),
      Allowed: allowedBinaries(cfg),
    }
  }

  args, err := guardArgs(in.Binary, in.Args)
  if err != nil {
    return nil, &RunCommandError{ Error: err.Error() }
  }

  // Path arguments are jailed like every other tool's paths.
  for _, p := range PathArgs(in.Binary, in.Args) {
    _, err := cfg.ResolvePath(p)
    if err != nil {
      return nil, &RunCommandError{ Error: err.Error() }
    }
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
  if readsContent[binary] {
    for _, a := range args {
      if strings.HasSuffix(strings.ToLower(a), ".pdf") {
        return nil, fmt.Errorf(
          "%s is a PDF (compressed binary; %s can't read it): use " +
            "read_pdf with path %q instead", a, binary, a,
        )
      }
    }
  }

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

// textOnly binaries take no paths; their arguments are text, and a
// "/" in them (tr / _) means nothing.
var textOnly = map[string]bool{
  "echo": true, "tr": true, "date": true, "uname": true, "which": true,
}

// readsContent binaries print or search a file's bytes: useless on a
// PDF, which is compressed.
var readsContent = map[string]bool{
  "cat": true, "head": true, "tail": true, "grep": true, "rg": true,
  "sed": true, "nl": true, "wc": true, "sort": true, "uniq": true,
  "cut": true, "diff": true,
}

// PathArgs picks the arguments of a run_command call that look like
// paths — ".", "..", anything with a "/" or starting with "~/", and
// the value of a --flag=path — so they can be jailed. A pattern
// that merely looks like a path ("a/b") resolves inside the project
// and passes.
func PathArgs(binary string, args []string) []string {
  if textOnly[binary] {
    return nil
  }

  var out []string
  for _, a := range args {
    if strings.HasPrefix(a, "-") {
      _, v, ok := strings.Cut(a, "=")
      if !ok {
        continue
      }

      a = v
    }

    if a == "." || a == ".." || strings.Contains(a, "/") || strings.HasPrefix(a, "~") {
      out = append(out, expandHome(a))
    }
  }

  return out
}

func expandHome(p string) string {
  if p == "~" || strings.HasPrefix(p, "~/") {
    home, err := os.UserHomeDir()
    if err == nil {
      return filepath.Join(home, p[1:])
    }
  }

  return p
}

func allowedBinaries(cfg *sandbox.Config) []string {
  allowed := make([]string, 0, len(cfg.Allowlist))
  for b := range cfg.Allowlist {
    allowed = append(allowed, b)
  }

  sort.Strings(allowed)

  return allowed
}
