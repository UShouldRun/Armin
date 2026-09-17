// Package sandbox holds the safety boundaries that are enforced in
// code, not left to model judgment: path jail, binary allowlist,
// subprocess timeouts, output truncation, and dry-run.
package sandbox

import (
  "bytes"
  "context"
  "fmt"
  "os/exec"
  "path/filepath"
  "strings"
  "time"
)

// Config is the set of safety rules a Sandbox enforces. Construct one
// per project root and pass it to every tool.
type Config struct {
  // Root is the absolute, symlink-resolved project root. No path
  // argument may resolve outside of it.
  Root string

  // Allowlist is the set of binaries run_command is permitted to
  // exec. Extend this deliberately; never let the model extend it.
  Allowlist map[string]bool

  // Timeout bounds every subprocess call.
  Timeout time.Duration

  // MaxOutput is the byte cap applied to stdout/stderr before it's
  // handed back to the model.
  MaxOutput int

  // DryRun, when set, makes Run describe the command it would have
  // executed instead of executing it. Wired up by run_command so you
  // can trust the loop before letting it actually touch a subprocess.
  DryRun bool
}

// DefaultAllowlist is a conservative starting set: read-only
// inspection tools. No write-capable commands (rm, mv, redirection)
// belong here.
func DefaultAllowlist() map[string]bool {
  return map[string]bool{
    "rg":   true,
    "grep": true,
    "find": true,
    "git":  true,
    "go":   true,
    "cat":  true,
    "ls":   true,
    "wc":   true,
  }
}

// Options configures New. Zero-value Options is a workable default:
// empty root resolves to ".", zero Timeout/MaxOutput fall back to
// sane defaults so callers that only care about the path jail don't
// have to spell everything out.
type Options struct {
  Timeout   time.Duration
  MaxOutput int
  Allowlist map[string]bool
  DryRun    bool
}

// New resolves root to an absolute, symlink-resolved path and returns
// a Config ready to use.
func New(root string, opts Options) (*Config, error) {
  abs, err := filepath.Abs(root)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in New:\n\t%w", err)
  }

  resolved, err := filepath.EvalSymlinks(abs)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in New:\n\t%w", err)
  }

  allowlist := opts.Allowlist
  if allowlist == nil {
    allowlist = DefaultAllowlist()
  }

  timeout := opts.Timeout
  if timeout == 0 {
    timeout = 20 * time.Second
  }

  maxOutput := opts.MaxOutput
  if maxOutput == 0 {
    maxOutput = 4000
  }

  return &Config{
    Root:      resolved,
    Allowlist: allowlist,
    Timeout:   timeout,
    MaxOutput: maxOutput,
    DryRun:    opts.DryRun,
  }, nil
}

// ResolvePath jails a path argument to the project root. It resolves
// the path (relative paths are taken as relative to Root), follows
// symlinks, and rejects anything that escapes Root — including
// symlink escapes, since EvalSymlinks runs before the prefix check.
func (c *Config) ResolvePath(path string) (string, error) {
  if path == "" {
    path = "."
  }

  var candidate string
  if filepath.IsAbs(path) {
    candidate = path
  } else {
    candidate = filepath.Join(c.Root, path)
  }
  candidate = filepath.Clean(candidate)

  // EvalSymlinks requires the path to exist. Walk up to the first
  // existing ancestor so we can still jail-check paths that don't
  // exist yet (useful for clearer error messages; write tools are
  // out of scope for v1 but this keeps the check honest either way).
  resolved, err := resolveExistingPrefix(candidate)
  if err != nil {
    return "", fmt.Errorf("\n\t... in ResolvePath:\n\t%w", err)
  }

  rel, err := filepath.Rel(c.Root, resolved)
  escapes := err != nil || rel == ".." ||
    strings.HasPrefix(rel, ".."+string(filepath.Separator))
  if escapes {
    return "", fmt.Errorf(
      "\n\t... in ResolvePath:\n\t path %q escapes project root %q",
      path, c.Root,
    )
  }

  return resolved, nil
}

// resolveExistingPrefix evaluates symlinks on the longest existing
// prefix of path, then reattaches the remaining (possibly
// nonexistent) suffix.
func resolveExistingPrefix(path string) (string, error) {
  suffix := ""
  cur := path
  for {
    resolved, err := filepath.EvalSymlinks(cur)
    if err == nil {
      return filepath.Join(resolved, suffix), nil
    }

    parent := filepath.Dir(cur)
    if parent == cur {
      // Reached the filesystem root without finding anything that
      // exists.
      return "", err
    }

    suffix = filepath.Join(filepath.Base(cur), suffix)
    cur = parent
  }
}

// CheckBinary rejects any binary not explicitly in the allowlist.
func (c *Config) CheckBinary(bin string) error {
  // Never allow a path-qualified binary; only bare names we've
  // explicitly allowlisted, so PATH resolution is the only lookup
  // path and nobody can point us at an arbitrary executable via a
  // crafted absolute/relative path.
  if bin != filepath.Base(bin) {
    return fmt.Errorf(
      "\n\t... in CheckBinary:\n\t binary %q must be a bare name, not a path",
      bin,
    )
  }
  if !c.Allowlist[bin] {
    return fmt.Errorf(
      "\n\t... in CheckBinary:\n\t binary %q not in allowlist", bin,
    )
  }
  return nil
}

// RunFlags packs RunResult's boolean outcomes into one byte instead
// of separate bool fields. Field order matters for struct size in Go
// (fields are laid out in declaration order, then padded): a lone int
// sandwiched between two bools forces padding on both sides. With
// ExitCode moved before a single trailing flags byte, RunResult drops
// from 56 to 48 bytes — real, since one gets allocated per subprocess
// call, not just tidiness.
type RunFlags uint8

const (
  RunTruncated RunFlags = 1 << iota
  RunDryRun
  RunAll = RunTruncated | RunDryRun
)

func (f RunFlags) Truncated() bool { return f&RunTruncated != 0 }
func (f RunFlags) DryRun() bool    { return f&RunDryRun != 0 }

// RunResult is the outcome of a sandboxed subprocess call.
type RunResult struct {
  Stdout, Stderr string
  ExitCode       int
  Flags          RunFlags
}

// Run execs binary with args directly — never through a shell — under
// the configured timeout, and truncates output to MaxOutput bytes.
// Callers are responsible for calling CheckBinary and ResolvePath
// first; Run itself doesn't re-check the allowlist so it can also be
// used for tools like search_code that wrap a fixed, trusted binary
// (ripgrep) rather than an arbitrary one from run_command.
//
// When c.DryRun is set, Run does not exec anything: it returns a
// RunResult describing what would have run. Callers that always want
// to actually run (e.g. search_code, which isn't the user-facing
// escape hatch run_command targets) should bypass DryRun by calling
// RunForce.
func (c *Config) Run(
  ctx context.Context, binary string, args []string,
) (*RunResult, error) {
  if c.DryRun {
    return &RunResult{
      Stdout: fmt.Sprintf(
        "[dry-run] would execute: %s", DescribeArgs(binary, args),
      ),
      Flags: RunDryRun,
    }, nil
  }
  return c.RunForce(ctx, binary, args)
}

// RunForce execs binary regardless of c.DryRun. See Run for the safe,
// dry-run-aware entry point most callers want.
func (c *Config) RunForce(
  ctx context.Context, binary string, args []string,
) (*RunResult, error) {
  ctx, cancel := context.WithTimeout(ctx, c.Timeout)
  defer cancel()

  cmd := exec.CommandContext(ctx, binary, args...)
  cmd.Dir = c.Root

  var stdout, stderr bytes.Buffer
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr

  err := cmd.Run()

  if ctx.Err() == context.DeadlineExceeded {
    return nil, fmt.Errorf(
      "\n\t... in RunForce:\n\t command %q timed out after %s",
      binary, c.Timeout,
    )
  }

  exitCode := 0
  if err != nil {
    exitErr, ok := err.(*exec.ExitError)
    if !ok {
      return nil, fmt.Errorf(
        "\n\t... in RunForce:\n\t running %q: %w", binary, err,
      )
    }
    exitCode = exitErr.ExitCode()
  }

  out, outTrunc := truncate(stdout.String(), c.MaxOutput)
  errOut, errTrunc := truncate(stderr.String(), c.MaxOutput)

  var flags RunFlags
  if outTrunc || errTrunc {
    flags |= RunTruncated
  }

  return &RunResult{
    Stdout:   out,
    Stderr:   errOut,
    ExitCode: exitCode,
    Flags:    flags,
  }, nil
}

// DescribeArgs renders binary + args the way they'd actually be
// exec'd, for dry-run output and error messages.
func DescribeArgs(binary string, args []string) string {
  return binary + " " + strings.Join(args, " ")
}

func truncate(s string, max int) (string, bool) {
  if len(s) <= max {
    return s, false
  }
  note := fmt.Sprintf(
    "\n... [truncated, %d more bytes — narrow your query to see the rest]",
    len(s)-max,
  )
  return s[:max] + note, true
}
