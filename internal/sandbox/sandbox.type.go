package sandbox

import (
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

  // ExecAllowlist is what run_program may start: compilers, build
  // tools, test runners. Unlike Allowlist these write files and run
  // project code, so every run is approved by the user.
  ExecAllowlist map[string]bool

  // Env, if set, is the environment every command gets — armin's
  // own minus API keys and cloud credentials, which a Makefile or a
  // test has no business seeing.
  Env func() []string
}

// Commands holds user-configured project commands as argv; nil
// means "detect it".
type Commands struct {
  Test, Build, Lint []string
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

  ExecAllowlist map[string]bool
  Commands     Commands
}

// OutsideError is a path outside the project and every granted
// directory. Callers that can ask the user (the agent loop) look for
// it with errors.As and offer to grant Dir.
type OutsideError struct {
  Path     string // as given
  Resolved string // absolute, symlinks resolved
  Root     string
}

// RunFlags packs RunResult's boolean outcomes into one byte instead
// of separate bool fields. Field order matters for struct size in Go
// (fields are laid out in declaration order, then padded): a lone int
// sandwiched between two bools forces padding on both sides. With
// ExitCode moved before a single trailing flags byte, RunResult drops
// from 56 to 48 bytes — real, since one gets allocated per subprocess
// call, not just tidiness.
type RunFlags uint8

// RunResult is the outcome of a sandboxed subprocess call.
type RunResult struct {
  Stdout, Stderr string
  ExitCode       int
  Flags          RunFlags
}
