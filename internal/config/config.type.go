// The types of config.go.

package config

import (
  "time"
)

// ModelSpecConfig configures one named model /model (or armin.model)
// can switch to — see internal/llamacpp.ModelSpec, which this is
// converted to. It's either external (URL set: armin only talks to
// the process there, never starts or stops it) or armin-managed
// (Binary/ModelPath/Port set: armin spawns a llama-server itself and
// reaps it when idle). Setting both isn't rejected here — this
// package does no cross-field validation, matching the rest of its
// minimalism — URL simply wins, same as
// internal/llamacpp.ModelSpec.managed() decides it.
//
// APIKeyEnv names an environment variable (see LoadEnv/EnvPath
// below) rather than holding a key value directly — config.conf is
// plain text people back up and share, so this package never reads
// or stores an actual secret, only the name of where to find one at
// request time. cmd/armin/main.go resolves it via os.Getenv once,
// when building internal/llamacpp.ModelSpec.APIKey.
type ModelSpecConfig struct {
  URL       string
  APIKeyEnv string
  ID        string // the hosted API's name for the model
  Reasoning string // a hosted model's reasoning effort
  Region    string // openrouter host: auto (by time zone), eu, us, off
  Private   bool   // openrouter: only providers that keep nothing

  Binary    string
  ModelPath string
  Port      int
  Args      []string

  IdleTimeout time.Duration

  // Remote machine (see llamacpp.RemoteSpec): an EC2 instance or a
  // STACKIT server runs Binary/ModelPath; Port is then the local end
  // of the tunnel.
  AWSInstance, AWSRegion                       string
  StackitServer, StackitProject, StackitRegion string
  ScalewayServer, ScalewayZone                 string
  SSHUser, SSHKey                              string
  RemotePort                                   int
  KeepOnDisconnect                             bool // stop_on_disconnect = false
  Vision                                       bool // keep the --mmproj projector
  NoLean                                       bool // lean = false

  // Streaming tier (see llamacpp.StreamSpec).
  StreamBinary   string
  StreamContext  int
  StreamArenaMiB int
  StreamArgs     []string
}

// Config holds every value a config file may override. Zero values
// mean "not set" — callers apply their own defaults on top.
type Config struct {
  Model           string // a name, resolved against Models (or LlamaURL, for the zero-config path)
  CompletionModel string // same, but for -lsp inline completion, not chat
  Terminal        string // how to open a new terminal window: "ghostty -e"
  CtagsBinary     string // used only by -lsp definition/symbol/hover

  // LlamaURL/CompletionLlamaURL back the zero-config path: point
  // armin at an already-running llama-server without writing a
  // Models entry at all. Ignored for a name that does have a
  // matching Models entry.
  LlamaURL           string
  CompletionLlamaURL string

  // Models holds every "models.<name>.<field>" entry, keyed by
  // name — the fuller alternative to LlamaURL/CompletionLlamaURL,
  // needed for anything armin should spawn/manage itself, or for
  // naming more than the two built-in chat/completion slots.
  Models map[string]ModelSpecConfig

  Root         string
  Timeout      time.Duration
  MaxOutput    int
  ExtraAllowed []string // appended to sandbox.DefaultAllowlist()
  ExtraExec    []string // appended to sandbox.DefaultExecAllowlist()

  // Agent behavior. Zero means "use the default".
  MaxToolRounds int
  ContextSize   int // tokens; overrides what llama-server reports
  KeepTurns     int // turns that keep full tool results; -1 never trims
  ReadTimeout   time.Duration
  BuildTimeout  time.Duration
  AutoApprove   bool

  // Library is a folder of reference books and docs the agent
  // searches with library_search (see internal/library).
  Library string

  // Effort is how hard models think by default: auto (the model
  // judges each request), off, low, medium or high.
  Effort string

  // Aids are the reasoning aids: all, none, or a list of stuck,
  // procedure, verify.
  Aids string

  // Project commands as argv, overriding detection.
  TestCmd, BuildCmd, LintCmd []string
}
