package llamacpp

import (
  "os/exec"
  "sync"
  "time"

  "armin/internal/cloud"
)

// ModelSpec configures one named model /model (or armin.model) can
// switch to.
type ModelSpec struct {
  // URL, if set, marks this spec externally managed: armin only
  // ever talks to it, never starts or stops the process behind it.
  // Mutually exclusive with Binary/ModelPath.
  URL string

  // ID is the name a hosted API knows the model by, sent in
  // requests in place of armin's alias.
  ID string

  // Effort is a hosted reasoning model's effort ("low", ...), sent
  // with each request; empty leaves the provider's default.
  Effort string

  // APIKeyEnv, if set, names an environment variable whose value is
  // sent as this model's every request's "Authorization: Bearer
  // <value>" header — see llm.Client.APIKey. Only meaningful
  // alongside URL: a locally spawned llama-server needs no auth.
  // Resolved via os.Getenv at request-client-construction time
  // (Manager.apiKey), not stored as a value here — this Manager also
  // strips every name configured this way from spawned llama-server
  // processes' environment (see scrubbedEnviron), so one model's key
  // never reaches an unrelated local process that inherits the
  // environment by default otherwise.
  APIKeyEnv string

  // Binary is the llama-server executable to spawn (looked up on
  // PATH if not absolute) — armin owns this process's lifecycle.
  Binary string
  // ModelPath is the .gguf file passed as llama-server's -m flag.
  ModelPath string
  // Port is the localhost port llama-server is told to listen on
  // (--port) and the one this package talks to it over.
  Port int
  // Args is extra flags appended after -m/--port, verbatim (e.g.
  // "--ctx-size", "8192", "--n-gpu-layers", "999").
  Args []string
  // IdleTimeout overrides DefaultIdleTimeout for this model; a
  // negative value means "never idle-stop this one".
  IdleTimeout time.Duration

  // Flags opt out of serverArgs' memory-saving defaults.
  Flags SpecFlags

  // Stream, if set, is a second, larger context tier: a llama-server
  // build with adaptive KV streaming, which keeps a resident pool and
  // a ring buffer in VRAM and the rest of the KV cache in pinned RAM.
  // agent.Loop moves between tiers through Window.
  Stream *StreamSpec

  // Remote, if set, runs Binary/ModelPath/Args on a cloud machine
  // instead of locally: armin starts it, launches llama-server over
  // SSH with a tunnel to Port, and stops it again when done (see
  // remote.go).
  Remote *RemoteSpec
}

// StreamSpec configures the streaming tier.
type StreamSpec struct {
  Binary   string   // the fork's llama-server; empty reuses Binary
  Context  int      // window in this tier
  ArenaMiB int      // --kv-stream-arena-mib: VRAM pool + ring buffer
  Args     []string // extra flags for this tier only
}

// Tier is which context tier a model's process runs in.
type Tier uint8

// SpecFlags are per-model switches, packed into one byte.
type SpecFlags uint8

// ModelStatus is one entry Names returns — enough for /model (and
// armin.model) with no argument to show what's configured without
// touching any process.
type ModelStatus struct {
  Name    string
  Model   string // what actually serves it: the .gguf name, a machine, a URL
  Managed bool
  Running bool
}

// process tracks one spawned (armin-managed) llama-server — or, for
// an AWS model, the SSH session running it.
type process struct {
  cmd      *exec.Cmd
  done     chan struct{} // closed once cmd exits; nil if never started
  out      *tailBuffer
  remote   *remoteRun
  lastUsed time.Time
  refcount int
}

// remoteRun is the cloud side of a remote process.
type remoteRun struct {
  machine cloud.Machine
  started bool // armin started the machine, so armin stops it
}

// tailBuffer keeps the last max bytes written — enough of a child's
// output to explain why it died, without holding all of it.
type tailBuffer struct {
  mu  sync.Mutex
  buf []byte
  max int
}

// Manager owns every configured ModelSpec and the armin-managed
// processes currently running for them. Zero value is not usable —
// construct with New.
type Manager struct {
  mu    sync.Mutex
  specs map[string]ModelSpec
  procs map[string]*process // only entries for currently-running managed specs

  // scrubEnvNames is every APIKeyEnv name configured across all
  // specs — computed once, at New, since specs never change after
  // construction. Spawned processes never see these regardless of
  // which model they belong to (see scrubbedEnviron): a local
  // llama-server has no legitimate reason to read any of them.
  scrubEnvNames map[string]bool

  stopReap chan struct{}
  reapDone chan struct{}
  shutdown sync.Once

  // tiers is each model's current tier; absent means resident.
  tiers map[string]Tier

  // owned is every EC2 instance armin started whose process has since
  // died, so a respawn onto the still-running instance keeps the
  // duty to stop it.
  owned map[string]*remoteRun

  // ReadTimeout is set on every Client this Manager hands out (see
  // llm.Client.ReadTimeout). Set it before the first Client call.
  ReadTimeout time.Duration

  // OnStatus, if set, hears progress on slow starts (an EC2 boot
  // takes minutes). Must not write to stdout in -lsp mode.
  OnStatus func(string)

  // Warning, when set, says why local models will be slow (see
  // GPUWarning), for every frontend to show.
  Warning string

  // KnownHostsFile is where SSH records remote host keys (keyed by
  // machine id, since a stopped machine comes back on a new IP).
  KnownHostsFile string

  // SSHBinary and the endpoints are overridden by tests.
  SSHBinary, EC2Endpoint, StackitEndpoint, ScalewayEndpoint string
}

// Window is one model's pair of context tiers, as agent.Loop sees
// them (it satisfies agent.WindowControl).
type Window struct {
  m              *Manager
  name           string
  resident, max  int
}
