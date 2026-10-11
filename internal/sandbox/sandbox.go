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

  // Commands overrides the detected test/build/lint commands (see
  // tools.DetectCommands). Like Allowlist, only the user sets these.
  Commands Commands

  // BuildTimeout bounds run_tests/build_check/lint_check, which
  // legitimately run far longer than an inspection command.
  BuildTimeout time.Duration

  // ReadRoots are extra directories the user granted read access to
  // (/add-dir): read tools may resolve paths under them; writes stay
  // jailed to Root (see InRoot).
  ReadRoots []string
}

// AddReadRoot grants read access to dir (resolved, symlinks
// followed) and returns the resolved path.
func (c *Config) AddReadRoot(dir string) (string, error) {
  abs, err := filepath.Abs(dir)
  if err != nil {
    return "", fmt.Errorf("\n\t... in AddReadRoot:\n\t%w", err)
  }

  resolved, err := filepath.EvalSymlinks(abs)
  if err != nil {
    return "", fmt.Errorf("\n\t... in AddReadRoot:\n\t%w", err)
  }

  for _, r := range c.ReadRoots {
    if r == resolved {
      return resolved, nil
    }
  }

  c.ReadRoots = append(c.ReadRoots, resolved)
  return resolved, nil
}

// within reports whether path (resolved) is base or under it.
func within(base, path string) bool {
  rel, err := filepath.Rel(base, path)
  return err == nil && rel != ".." &&
    !strings.HasPrefix(rel, ".." + string(filepath.Separator))
}

// InRoot reports whether a resolved path is inside the project
// itself — what every write must satisfy, whatever ReadRoots allow.
func (c *Config) InRoot(resolved string) bool {
  return within(c.Root, resolved)
}

// Commands holds user-configured project commands as argv; nil
// means "detect it".
type Commands struct {
  Test, Build, Lint []string
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
  Timeout      time.Duration
  BuildTimeout time.Duration
  MaxOutput    int
  Allowlist    map[string]bool
  DryRun       bool
  Commands     Commands
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

  buildTimeout := opts.BuildTimeout
  if buildTimeout == 0 {
    buildTimeout = 10 * time.Minute
  }

  return &Config{
    Root:         resolved,
    Allowlist:    allowlist,
    Timeout:      timeout,
    BuildTimeout: buildTimeout,
    MaxOutput:    maxOutput,
    DryRun:       opts.DryRun,
    Commands:     opts.Commands,
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

  if within(c.Root, resolved) {
    return resolved, nil
  }

  for _, r := range c.ReadRoots {
    if within(r, resolved) {
      return resolved, nil
    }
  }

  return "", fmt.Errorf(
    "\n\t... in ResolvePath:\n\t path %q escapes project root %q "+
      "(the user can grant read access with /add-dir)",
    path, c.Root,
  )
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

func (f RunFlags) Truncated() bool { return f & RunTruncated != 0 }
func (f RunFlags) DryRun() bool    { return f & RunDryRun != 0 }

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
  stdout, stderr, exitCode, err := c.runRaw(ctx, "RunForce", binary, args)
  if err != nil {
    return nil, err
  }

  out, outTrunc := truncate(stdout, c.MaxOutput)
  errOut, errTrunc := truncate(stderr, c.MaxOutput)

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

// RunForceFull is RunForce without MaxOutput truncation — for the
// rare caller (module_graph) that has to parse the whole output as
// structured data (JSON) afterward, where a truncated stream isn't
// just shorter, it's invalid and fails to parse. Still bounded by
// c.Timeout; just not by c.MaxOutput. Use RunForce unless truncation
// would actually corrupt what you're about to do with the output.
func (c *Config) RunForceFull(
  ctx context.Context, binary string, args []string,
) (*RunResult, error) {
  stdout, stderr, exitCode, err := c.runRaw(ctx, "RunForceFull", binary, args)
  if err != nil {
    return nil, err
  }
  return &RunResult{ Stdout: stdout, Stderr: stderr, ExitCode: exitCode }, nil
}

// RunBuild is RunForceFull under BuildTimeout instead of Timeout,
// for test/build/lint runs. Output is untruncated: the caller keeps
// the tail, where failures are reported, rather than the head.
func (c *Config) RunBuild(
  ctx context.Context, binary string, args []string,
) (*RunResult, error) {
  stdout, stderr, exitCode, err := c.runWith(
    ctx, "RunBuild", c.BuildTimeout, binary, args,
  )
  if err != nil {
    return nil, err
  }
  return &RunResult{ Stdout: stdout, Stderr: stderr, ExitCode: exitCode }, nil
}

func (c *Config) runRaw(
  ctx context.Context, callerName, binary string, args []string,
) (stdout, stderr string, exitCode int, err error) {
  return c.runWith(ctx, callerName, c.Timeout, binary, args)
}

func (c *Config) runWith(
  ctx context.Context, callerName string, timeout time.Duration,
  binary string, args []string,
) (stdout, stderr string, exitCode int, err error) {
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()

  cmd := exec.CommandContext(ctx, binary, args...)
  cmd.Dir = c.Root

  var outBuf, errBuf bytes.Buffer
  cmd.Stdout = &outBuf
  cmd.Stderr = &errBuf

  runErr := cmd.Run()

  if ctx.Err() == context.DeadlineExceeded {
    return "", "", 0, fmt.Errorf(
      "\n\t... in %s:\n\t command %q timed out after %s",
      callerName, binary, timeout,
    )
  }

  if runErr != nil {
    exitErr, ok := runErr.(*exec.ExitError)
    if !ok {
      return "", "", 0, fmt.Errorf(
        "\n\t... in %s:\n\t running %q: %w", callerName, binary, runErr,
      )
    }
    exitCode = exitErr.ExitCode()
  }

  return outBuf.String(), errBuf.String(), exitCode, nil
}

// DescribeArgs renders binary + args the way they'd actually be
// exec'd, for dry-run output and error messages.
func DescribeArgs(binary string, args []string) string {
  return binary + " " + strings.Join(args, " ")
}

// TruncateTail keeps the last max bytes of s — for build and test
// output, where the verdict and the failures come last.
func TruncateTail(s string, max int) (string, bool) {
  if len(s) <= max {
    return s, false
  }
  cut := len(s) - max
  if i := strings.IndexByte(s[cut:], '\n'); i >= 0 && i < 200 {
    cut += i + 1
  }
  note := fmt.Sprintf(
    "[... %d earlier bytes truncated — the end is kept]\n", cut,
  )
  return note + s[cut:], true
}

func truncate(s string, max int) (string, bool) {
  if len(s) <= max {
    return s, false
  }
  note := fmt.Sprintf(
    "\n... [truncated, %d more bytes — narrow your query to see the rest]",
    len(s) - max,
  )
  return s[:max] + note, true
}
