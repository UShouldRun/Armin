// Package sandbox holds the safety boundaries that are enforced in
// code, not left to model judgment: path jail, binary allowlist,
// subprocess timeouts, output truncation, and dry-run.
package sandbox

import (
  "bytes"
  "context"
  "fmt"
  "io"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"
)

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

// DefaultAllowlist is a conservative starting set: read-only
// inspection tools. No write-capable commands (rm, mv, redirection)
// belong here.
//
// Not here on purpose: cd and exit (no shell, so they do nothing —
// every command runs in Root), env and printenv (they would print
// API keys), and anything that writes. sed and find are allowed but
// argument-checked (see tools.RunCommand).
func DefaultAllowlist() map[string]bool {
  return map[string]bool{
    "rg": true, "grep": true, "find": true, "git": true, "go": true,
    "cat": true, "ls": true, "wc": true, "head": true, "tail": true,
    "sed": true, "sort": true, "uniq": true, "cut": true, "tr": true,
    "diff": true, "stat": true, "file": true, "tree": true,
    "du": true, "df": true, "pwd": true, "echo": true, "which": true,
    "uname": true, "date": true, "basename": true, "dirname": true,
    "realpath": true, "nl": true, "sha256sum": true, "md5sum": true,
  }
}

// DefaultExecAllowlist is run_program's starting set: build systems,
// compilers, MPI launchers and test runners. Package managers that
// install from the network (pip, npx, go install) aren't in it.
func DefaultExecAllowlist() map[string]bool {
  out := map[string]bool{}
  for _, b := range []string{
    "make", "cmake", "ninja", "meson", "ctest",
    "gcc", "g++", "cc", "c++", "clang", "clang++", "ld", "ar",
    "mpicc", "mpicxx", "mpic++", "mpirun", "mpiexec",
    "go", "gofmt", "cargo", "rustc", "zig",
    "python3", "python", "pytest",
    "node", "npm", "pnpm", "yarn", "bun", "deno", "tsc",
    "javac", "java", "mvn", "gradle", "dotnet",
    "typst", "pdflatex", "latexmk",
    "valgrind", "gdb", "time",
    "rm", "cp", "mv", "scp", "rsync",
    "bash", "sh",
  } {
    out[b] = true
  }

  return out
}

// privileged programs delete, overwrite or move files, or send them
// to another machine: every run is asked, even when the user
// approved everything else for the session.
var privileged = map[string]bool{
  "rm": true, "cp": true, "mv": true, "scp": true, "rsync": true,
  "bash": true, "sh": true,
}

// PrivilegedWhy is why bin is always asked about.
func PrivilegedWhy(bin string) string {
  switch filepath.Base(bin) {
  case "bash", "sh":
    return "a shell can run anything"
  }

  return "deletes, moves, copies or sends files"
}

// Privileged reports whether bin is always asked about.
func Privileged(bin string) bool {
  return privileged[filepath.Base(bin)]
}

// CheckExec allows an ExecAllowlist name, or a program inside the
// project given as a path relative to dir ("./fox", "bin/test.sh"):
// what the project itself built.
func (c *Config) CheckExec(bin, dir string) (string, error) {
  if bin == filepath.Base(bin) {
    if !c.ExecAllowlist[bin] {
      return "", fmt.Errorf(
        "\n\t... in CheckExec:\n\t %q isn't in run_program's allowlist " +
          "(the user can add it with allow_exec in config.conf)", bin,
      )
    }

    return bin, nil
  }

  if filepath.IsAbs(bin) {
    return "", fmt.Errorf(
      "\n\t... in CheckExec:\n\t %q: give programs inside the project " +
        "as relative paths (./build/x); outside ones aren't allowed", bin,
    )
  }

  resolved, err := c.ResolvePath(filepath.Join(dir, bin))
  if err != nil || !c.InRoot(resolved) {
    return "", fmt.Errorf(
      "\n\t... in CheckExec:\n\t %q isn't a program inside the project", bin,
    )
  }

  info, err := os.Stat(resolved)
  if err != nil || info.IsDir() || info.Mode() & 0o111 == 0 {
    return "", fmt.Errorf(
      "\n\t... in CheckExec:\n\t %q doesn't exist or isn't executable " +
        "(build it first)", bin,
    )
  }

  return resolved, nil
}

// RunIn is RunBuild in dir, a directory inside the project.
func (c *Config) RunIn(
  ctx context.Context, dir, binary string, args []string,
) (*RunResult, error) {
  stdout, stderr, exitCode, err := c.runAt(
    ctx, "RunIn", c.BuildTimeout, dir, binary, args,
  )
  if err != nil {
    return nil, err
  }

  return &RunResult{ Stdout: stdout, Stderr: stderr, ExitCode: exitCode }, nil
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

  execAllowlist := opts.ExecAllowlist
  if execAllowlist == nil {
    execAllowlist = DefaultExecAllowlist()
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

    ExecAllowlist: execAllowlist,
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
    "\n\t... in ResolvePath:\n\t%w",
    &OutsideError{ Path: path, Resolved: resolved, Root: c.Root },
  )
}

func (e *OutsideError) Error() string {
  return fmt.Sprintf(
    "path %q is outside the project %q and not readable yet: call "+
      "request_access with its directory to ask the user for read access",
    e.Path, e.Root,
  )
}

// Dir is the directory to grant for the path: itself if it is one,
// else its parent.
func (e *OutsideError) Dir() string {
  info, err := os.Stat(e.Resolved)
  if err == nil && info.IsDir() {
    return e.Resolved
  }

  return filepath.Dir(e.Resolved)
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

const (
  RunTruncated RunFlags = 1 << iota
  RunDryRun
  RunAll = RunTruncated | RunDryRun
)

func (f RunFlags) Truncated() bool { return f & RunTruncated != 0 }
func (f RunFlags) DryRun() bool    { return f & RunDryRun != 0 }

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
  return c.runAt(ctx, callerName, timeout, c.Root, binary, args)
}

// RunInWith is RunIn with the program's input read from stdin and,
// when stdout is set, its output written there instead of returned.
func (c *Config) RunInWith(
  ctx context.Context, dir, binary string, args []string,
  stdin io.Reader, stdout io.Writer,
) (*RunResult, error) {
  out, errOut, exitCode, err := c.runIO(
    ctx, "RunInWith", c.BuildTimeout, dir, binary, args, stdin, stdout,
  )
  if err != nil {
    return nil, err
  }

  return &RunResult{ Stdout: out, Stderr: errOut, ExitCode: exitCode }, nil
}

func (c *Config) runAt(
  ctx context.Context, callerName string, timeout time.Duration,
  dir, binary string, args []string,
) (stdout, stderr string, exitCode int, err error) {
  return c.runIO(ctx, callerName, timeout, dir, binary, args, nil, nil)
}

func (c *Config) runIO(
  ctx context.Context, callerName string, timeout time.Duration,
  dir, binary string, args []string, stdin io.Reader, stdout io.Writer,
) (outText, errText string, exitCode int, err error) {
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()

  cmd := exec.CommandContext(ctx, binary, args...)
  cmd.Dir = dir
  // A killed make can leave a compiler holding the output pipes;
  // don't wait on it forever.
  cmd.WaitDelay = 5 * time.Second
  env := os.Environ()
  if c.Env != nil {
    env = c.Env()
  }

  cmd.Env = noPrompt(env)
  detachTerminal(cmd)

  var outBuf, errBuf bytes.Buffer
  cmd.Stdin = stdin
  cmd.Stdout = &outBuf
  if stdout != nil {
    cmd.Stdout = stdout
  }

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

// noPrompt makes git fail with an error rather than wait for an
// answer nobody can give: no username/password prompt, and ssh in
// batch mode (no passphrase or unknown-host-key questions). A
// GIT_SSH_COMMAND the user already set is kept: replacing it would
// drop their key or wrapper.
func noPrompt(env []string) []string {
  env = append(env[:len(env):len(env)], "GIT_TERMINAL_PROMPT=0")
  for _, kv := range env {
    if strings.HasPrefix(kv, "GIT_SSH_COMMAND=") {
      return env
    }
  }

  return append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
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
  i := strings.IndexByte(s[cut:], '\n')
  if i >= 0 && i < 200 {
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
