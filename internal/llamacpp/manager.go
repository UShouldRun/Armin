// Package llamacpp manages the lifecycle of local llama.cpp
// llama-server processes and hands out internal/llm.Client
// instances pointed at whichever URL currently serves a given named
// model. Unlike Ollama (a single daemon juggling many pulled
// models), llama-server loads exactly one model per process — so
// "switching models" here means pointing at a different URL, and
// possibly spawning the process that serves it, rather than sending
// a different model name to a shared daemon.
//
// A ModelSpec is either externally managed (just a URL — something
// the user already runs themselves, e.g. a beefy always-on box;
// this package never starts or stops it) or armin-managed (a
// binary + .gguf path + port; this package spawns it on first use
// and — mirroring internal/searxng's Docker container lifecycle —
// stops only what it started, an idle process it spawned itself,
// never one the user already had running). A managed process left
// idle (no in-flight request) past its IdleTimeout is stopped by a
// background reaper and respawned lazily on next use, the same
// keep-it-loaded-only-while-busy tradeoff Ollama's own default
// keep_alive unload already makes.
package llamacpp

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "strings"
  "sync"
  "syscall"
  "time"

  "armin/internal/llm"
)

// DefaultIdleTimeout is how long a managed process may sit unused
// before the reaper stops it. A ModelSpec.IdleTimeout of zero uses
// this; a negative value disables idle shutdown for that model.
const DefaultIdleTimeout = 20 * time.Minute

// reapInterval is how often the background reaper checks every
// managed process against its idle timeout.
const reapInterval = 30 * time.Second

const (
  spawnHealthTimeout = 90 * time.Second // loading a large .gguf can be slow
  healthPoll         = 300 * time.Millisecond
  stopGraceTimeout   = 10 * time.Second
)

// ModelSpec configures one named model /model (or armin.model) can
// switch to.
type ModelSpec struct {
  // URL, if set, marks this spec externally managed: armin only
  // ever talks to it, never starts or stops the process behind it.
  // Mutually exclusive with Binary/ModelPath.
  URL string

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
}

func (s ModelSpec) managed() bool { return s.URL == "" }

func (s ModelSpec) idleTimeout() time.Duration {
  if s.IdleTimeout != 0 {
    return s.IdleTimeout
  }
  return DefaultIdleTimeout
}

func (s ModelSpec) url() string {
  if !s.managed() {
    return s.URL
  }
  return fmt.Sprintf("http://127.0.0.1:%d", s.Port)
}

// ModelStatus is one entry Names returns — enough for /model (and
// armin.model) with no argument to show what's configured without
// touching any process.
type ModelStatus struct {
  Name    string
  Managed bool
  Running bool
}

// process tracks one spawned (armin-managed) llama-server instance.
type process struct {
  cmd      *exec.Cmd
  refcount int
  lastUsed time.Time
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
}

// New builds a Manager around specs and starts its idle-reaping
// goroutine — nothing is spawned yet, only Client/Switch do that,
// lazily, on first use of a managed model.
func New(specs map[string]ModelSpec) *Manager {
  scrub := make(map[string]bool)
  for _, s := range specs {
    if s.APIKeyEnv != "" {
      scrub[s.APIKeyEnv] = true
    }
  }

  m := &Manager{
    specs:         specs,
    procs:         make(map[string]*process),
    scrubEnvNames: scrub,
    stopReap:      make(chan struct{}),
    reapDone:      make(chan struct{}),
  }
  go m.reapLoop()
  return m
}

// Names lists every configured model, alphabetically undefined
// order (callers sort if they need stable output), with whether
// armin currently has a managed one running.
func (m *Manager) Names() []ModelStatus {
  m.mu.Lock()
  defer m.mu.Unlock()

  out := make([]ModelStatus, 0, len(m.specs))
  for name, spec := range m.specs {
    _, running := m.procs[name]
    out = append(out, ModelStatus{
      Name: name, Managed: spec.managed(), Running: running,
    })
  }
  return out
}

// Has reports whether name is a configured model.
func (m *Manager) Has(name string) bool {
  m.mu.Lock()
  defer m.mu.Unlock()
  _, ok := m.specs[name]
  return ok
}

// Client returns a freshly constructed *llm.Client pointed at name,
// ensuring its process is running (spawning and health-polling it
// first, if it's a managed spec not already up). The returned
// Client's OnRequestStart/OnRequestEnd are wired back into this
// Manager, so the idle reaper never stops name's process while a
// request made through this Client is in flight.
func (m *Manager) Client(ctx context.Context, name string) (*llm.Client, error) {
  url, err := m.ensure(ctx, name)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Client:\n\t%w", err)
  }
  return m.newClientFor(name, url), nil
}

// Switch mutates an existing *llm.Client in place to point at name,
// ensuring its process is running first. Client fields are mutated
// rather than the pointer being replaced because the REPL,
// agent.Loop, and lspserver all hold onto the very same *llm.Client
// — a switch has to be visible through all of them at once (see the
// doc comment on llm.Client.Model for the concurrency caveat this
// carries).
func (m *Manager) Switch(ctx context.Context, client *llm.Client, name string) error {
  url, err := m.ensure(ctx, name)
  if err != nil {
    return fmt.Errorf("\n\t... in Switch:\n\t%w", err)
  }
  client.BaseURL = url
  client.Model = name
  client.APIKey = m.apiKey(name)
  client.OnRequestStart, client.OnRequestEnd = m.callbacks(name)
  return nil
}

func (m *Manager) newClientFor(name, url string) *llm.Client {
  c := llm.New(url, name)
  c.APIKey = m.apiKey(name)
  c.OnRequestStart, c.OnRequestEnd = m.callbacks(name)
  return c
}

// apiKey resolves name's configured APIKeyEnv against the real
// environment — called from newClientFor/Switch rather than reading
// m.specs directly, so neither has to remember to take m.mu itself.
// Resolved fresh on every call (not cached from New) so a key
// rotated in .env/the environment during a long-running session
// takes effect on the next switch, not just at startup.
func (m *Manager) apiKey(name string) string {
  m.mu.Lock()
  envName := m.specs[name].APIKeyEnv
  m.mu.Unlock()
  return os.Getenv(envName)
}

func (m *Manager) callbacks(name string) (start, end func()) {
  return func() { m.touch(name, 1) }, func() { m.touch(name, -1) }
}

// ensure resolves name to a URL, spawning and health-polling its
// process first if it's a managed spec that isn't already running.
func (m *Manager) ensure(ctx context.Context, name string) (string, error) {
  m.mu.Lock()
  spec, ok := m.specs[name]
  if !ok {
    m.mu.Unlock()
    return "", fmt.Errorf("unconfigured model %q", name)
  }
  if !spec.managed() {
    m.mu.Unlock()
    return spec.url(), nil
  }
  if proc, running := m.procs[name]; running {
    proc.lastUsed = time.Now()
    m.mu.Unlock()
    return spec.url(), nil
  }
  m.mu.Unlock()

  if err := m.spawn(ctx, name, spec); err != nil {
    return "", err
  }
  return spec.url(), nil
}

func (m *Manager) spawn(ctx context.Context, name string, spec ModelSpec) error {
  args := append([]string{ "-m", spec.ModelPath, "--port", fmt.Sprint(spec.Port) }, spec.Args...)
  cmd := exec.Command(spec.Binary, args...)
  // Independent of ctx on purpose: this process must outlive
  // whatever single request triggered spawning it. Its lifetime is
  // owned entirely by the reaper and Shutdown below.
  cmd.Env = m.scrubbedEnviron()
  if err := cmd.Start(); err != nil {
    return fmt.Errorf(
      "\n\t... in spawn:\n\t starting %q for %q: %w",
      spec.Binary, name, err,
    )
  }

  m.mu.Lock()
  m.procs[name] = &process{ cmd: cmd, lastUsed: time.Now() }
  m.mu.Unlock()

  if err := waitHealthy(ctx, spec.url()); err != nil {
    m.mu.Lock()
    delete(m.procs, name)
    m.mu.Unlock()
    stopProcess(cmd)
    return fmt.Errorf(
      "\n\t... in spawn:\n\t %q never became healthy: %w", name, err,
    )
  }
  return nil
}

// scrubbedEnviron is os.Environ() with every configured
// models.<name>.api_key_env name removed. exec.Cmd with a nil Env
// inherits the parent's environment in full, which would otherwise
// hand a Claude (or any other remote model's) API key to a local
// llama-server process that has no business seeing it.
func (m *Manager) scrubbedEnviron() []string {
  full := os.Environ()
  out := make([]string, 0, len(full))
  for _, kv := range full {
    name, _, _ := strings.Cut(kv, "=")
    if m.scrubEnvNames[name] {
      continue
    }
    out = append(out, kv)
  }
  return out
}

func waitHealthy(ctx context.Context, url string) error {
  ctx, cancel := context.WithTimeout(ctx, spawnHealthTimeout)
  defer cancel()

  client := llm.New(url, "")
  for {
    if err := client.Health(ctx); err == nil {
      return nil
    }
    select {
    case <-ctx.Done():
      return fmt.Errorf("timed out waiting for llama-server to come up")
    case <-time.After(healthPoll):
    }
  }
}

// touch records activity for name's managed process: delta is +1
// around the start of a request and -1 around its end, so the
// reaper can tell a genuinely idle process (refcount 0) from one
// mid-request regardless of how long that request takes. A no-op
// for externally managed specs, which have no entry in m.procs.
func (m *Manager) touch(name string, delta int) {
  m.mu.Lock()
  defer m.mu.Unlock()
  if proc, ok := m.procs[name]; ok {
    proc.refcount += delta
    proc.lastUsed = time.Now()
  }
}

func (m *Manager) reapLoop() {
  defer close(m.reapDone)
  ticker := time.NewTicker(reapInterval)
  defer ticker.Stop()

  for {
    select {
    case <-m.stopReap:
      return
    case <-ticker.C:
      m.reapIdle()
    }
  }
}

func (m *Manager) reapIdle() {
  now := time.Now()

  m.mu.Lock()
  var toStop []struct {
    name string
    cmd  *exec.Cmd
  }
  for name, proc := range m.procs {
    if proc.refcount > 0 {
      continue
    }
    spec := m.specs[name]
    timeout := spec.idleTimeout()
    if timeout < 0 {
      continue // idle shutdown disabled for this model
    }
    if now.Sub(proc.lastUsed) < timeout {
      continue
    }
    toStop = append(toStop, struct {
      name string
      cmd  *exec.Cmd
    }{ name, proc.cmd })
    delete(m.procs, name)
  }
  m.mu.Unlock()

  for _, p := range toStop {
    stopProcess(p.cmd)
  }
}

// Shutdown stops the reaper and every managed process this Manager
// spawned — but, mirroring internal/searxng.Manager.Shutdown, never
// touches an externally managed (URL-only) spec, since armin never
// started that process in the first place.
func (m *Manager) Shutdown(ctx context.Context) error {
  close(m.stopReap)
  <-m.reapDone

  m.mu.Lock()
  procs := m.procs
  m.procs = make(map[string]*process)
  m.mu.Unlock()

  for _, proc := range procs {
    stopProcess(proc.cmd)
  }
  return nil
}

// stopProcess sends SIGTERM and waits briefly for a clean exit,
// falling back to SIGKILL if the process ignores it — the same
// graceful-then-forced shape cmd/armin/main.go's own HTTP server
// shutdown uses.
func stopProcess(cmd *exec.Cmd) {
  if cmd.Process == nil {
    return
  }

  done := make(chan struct{})
  go func() {
    cmd.Wait()
    close(done)
  }()

  cmd.Process.Signal(syscall.SIGTERM)

  select {
  case <-done:
    return
  case <-time.After(stopGraceTimeout):
  }

  cmd.Process.Kill()
  <-done
}

