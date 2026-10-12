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
  "path/filepath"
  "strconv"
  "strings"
  "sync"
  "syscall"
  "time"

  "armin/internal/aws"
  "armin/internal/llm"
  "armin/internal/scaleway"
  "armin/internal/stackit"
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

const (
  TierResident Tier = 1 << iota // whole KV cache in VRAM: fastest
  TierStream                    // KV cache streamed from RAM: largest
  TierAll      = TierResident | TierStream
)

// contextFlags are llama-server's names for the window size.
var contextFlags = map[string]bool{ "-c": true, "--ctx-size": true }

// contextOf reads -c / --ctx-size out of args; 0 if absent.
func contextOf(args []string) int {
  for i, a := range args {
    name, val, hasVal := strings.Cut(a, "=")
    if !contextFlags[name] {
      continue
    }

    if !hasVal && i + 1 < len(args) {
      val = args[i + 1]
    }

    n, _ := strconv.Atoi(val)

    return n
  }

  return 0
}

// withContext returns args with the window set to n.
func withContext(args []string, n int) []string {
  out := make([]string, 0, len(args) + 2)
  for i := 0; i < len(args); i++ {
    name, _, hasVal := strings.Cut(args[i], "=")
    if contextFlags[name] {
      if !hasVal {
        i++ // skip the value too
      }

      continue
    }

    out = append(out, args[i])
  }

  return append(out, "-c", strconv.Itoa(n))
}

// inTier is the spec as it runs in t: the stream tier swaps in the
// fork's binary, its window and its arena.
func (s ModelSpec) inTier(t Tier) ModelSpec {
  if t != TierStream || s.Stream == nil {
    return s
  }

  st := s.Stream
  if st.Binary != "" {
    s.Binary = st.Binary
  }

  args := withContext(s.Args, st.Context)
  if st.ArenaMiB > 0 {
    args = append(args, "--kv-stream-arena-mib", strconv.Itoa(st.ArenaMiB))
  }

  s.Args = append(args, st.Args...)

  return s
}

const (
  // SpecVision keeps a multimodal projector (--mmproj) loadable.
  SpecVision SpecFlags = 1 << iota
  // SpecNoLean drops every default in leanDefaults.
  SpecNoLean
  SpecAll = SpecVision | SpecNoLean
)

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

// exited reports whether the process has already died.
func (p *process) exited() bool {
  if p.done == nil {
    return false
  }

  select {
  case <-p.done:
    return true
  default:
    return false
  }
}

// startProcess starts cmd with one goroutine owning cmd.Wait.
func startProcess(cmd *exec.Cmd) (*process, error) {
  out := &tailBuffer{ max: 4096 }
  cmd.Stdout, cmd.Stderr = out, out
  dieWithParent(cmd)
  err := cmd.Start()
  if err != nil {
    return nil, err
  }

  p := &process{ cmd: cmd, done: make(chan struct{}), out: out }
  go func() {
    cmd.Wait()
    close(p.done)
  }()

  return p, nil
}

func (t *tailBuffer) Write(b []byte) (int, error) {
  t.mu.Lock()
  defer t.mu.Unlock()
  t.buf = append(t.buf, b...)
  over := len(t.buf) - t.max
  if over > 0 {
    t.buf = append(t.buf[:0], t.buf[over:]...)
  }

  return len(b), nil
}

func (t *tailBuffer) String() string {
  if t == nil {
    return ""
  }

  t.mu.Lock()
  defer t.mu.Unlock()

  return strings.TrimSpace(string(t.buf))
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

  // No child process ever needs cloud credentials: armin makes the
  // API calls itself, and ssh authenticates with its own key.
  for _, name := range aws.CredentialEnv {
    scrub[name] = true
  }

  for _, name := range stackit.CredentialEnv {
    scrub[name] = true
  }

  for _, name := range scaleway.CredentialEnv {
    scrub[name] = true
  }

  m := &Manager{
    specs:         specs,
    procs:         make(map[string]*process),
    owned:         make(map[string]*remoteRun),
    tiers:         make(map[string]Tier),
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
      Name: name, Model: spec.label(),
      Managed: spec.managed(), Running: running,
    })
  }

  return out
}

// Label is what serves name, for display and the system prompt;
// "" for an unknown name.
func (m *Manager) Label(name string) string {
  m.mu.Lock()
  defer m.mu.Unlock()

  return m.specs[name].label()
}

// label names what serves the spec: the model file (without its
// directory and .gguf), plus the machine for a cloud one; the URL for
// an external endpoint.
func (s ModelSpec) label() string {
  if !s.managed() && s.ID != "" {
    return s.ID
  }

  if !s.managed() {
    return s.URL
  }

  file := strings.TrimSuffix(filepath.Base(s.ModelPath), ".gguf")
  if s.Remote != nil {
    return file + " on " + s.Remote.Provider.String() + " " + s.Remote.MachineID
  }

  return file
}

// OpenRouter is the configured model served through OpenRouter, if
// any: its endpoint and key reach every model OpenRouter offers.
func (m *Manager) OpenRouter() string {
  m.mu.Lock()
  defer m.mu.Unlock()

  best := ""
  for name, spec := range m.specs {
    hosted := strings.Contains(spec.URL, "openrouter.ai")
    if hosted && !strings.Contains(name, ":") && (best == "" || name < best) {
      best = name
    }
  }

  return best
}

// Endpoint is name's URL and resolved API key, for asking its
// provider about the account (see catalog.Credits).
func (m *Manager) Endpoint(name string) (url, key string) {
  m.mu.Lock()
  url = m.specs[name].URL
  m.mu.Unlock()

  return url, m.apiKey(name)
}

// adopt makes "base:id" a model of its own: base's endpoint, key and
// reasoning with another hosted id — "planner:deepseek/deepseek-v4-pro"
// tries any model a provider like OpenRouter offers without a config
// block per model. Only for a hosted (URL) base; a no-op otherwise.
func (m *Manager) adopt(name string) {
  base, id, ok := strings.Cut(name, ":")
  if !ok || id == "" {
    return
  }

  m.mu.Lock()
  defer m.mu.Unlock()

  _, known := m.specs[name]
  spec, found := m.specs[base]
  if known || !found || spec.URL == "" {
    return
  }

  spec.ID = id
  m.specs[name] = spec
}

// Has reports whether name is a configured model.
func (m *Manager) Has(name string) bool {
  m.adopt(name)
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
  // Two local models rarely fit one GPU together: free the one being
  // left before loading the next. Only that one — a separate
  // completion model may still be in use.
  old := client.Model
  if old != name && m.localManaged(old) && m.localManaged(name) {
    m.stopModel(ctx, old)
  }

  url, err := m.ensure(ctx, name)
  if err != nil {
    return fmt.Errorf("\n\t... in Switch:\n\t%w", err)
  }

  client.BaseURL = url
  client.Model = name
  client.ID, client.Effort = m.id(name)
  client.APIKey = m.apiKey(name)
  client.OnRequestStart, client.OnRequestEnd = m.callbacks(name)
  client.Ready = m.ready(name)

  return nil
}

// SwitchChat moves chat to name. follower, when set, is inline
// completion following chat — but only to a model on this machine:
// a cloud or external one would bill each keystroke and rarely
// serves /infill, so completion stays on the last local model. It
// switches first, so the old local model is gone before the new one
// loads (two rarely fit one GPU).
func (m *Manager) SwitchChat(
  ctx context.Context, chat, follower *llm.Client, name string,
) error {
  if follower != nil && follower.Model != name && m.localManaged(name) {
    err := m.Switch(ctx, follower, name)
    if err != nil {
      return fmt.Errorf("\n\t... in SwitchChat:\n\t%w", err)
    }
  }

  return m.Switch(ctx, chat, name)
}

// LazyClient is a Client for name that starts its server on first
// use rather than now.
func (m *Manager) LazyClient(name string) (*llm.Client, error) {
  m.adopt(name)
  m.mu.Lock()
  spec, ok := m.specs[name]
  m.mu.Unlock()
  if !ok {
    return nil, fmt.Errorf("unconfigured model %q", name)
  }

  return m.newClientFor(name, spec.url()), nil
}

// IsLocal reports whether name runs on this machine (see
// localManaged).
func (m *Manager) IsLocal(name string) bool { return m.localManaged(name) }

// FirstLocal is a model on this machine for inline completion:
// "local" when there is one, else the first by name; "" for none.
func (m *Manager) FirstLocal() string {
  m.mu.Lock()
  defer m.mu.Unlock()

  first := ""
  for name, spec := range m.specs {
    if !spec.managed() || spec.Remote != nil {
      continue
    }

    if name == "local" {
      return name
    }

    if first == "" || name < first {
      first = name
    }
  }

  return first
}

// localManaged reports whether name is a llama-server armin runs on
// this machine (not an external URL, not a cloud machine).
func (m *Manager) localManaged(name string) bool {
  m.mu.Lock()
  defer m.mu.Unlock()
  spec, ok := m.specs[name]

  return ok && spec.managed() && spec.Remote == nil
}

// stopModel stops name's process now, if it's running.
func (m *Manager) stopModel(ctx context.Context, name string) {
  m.mu.Lock()
  proc := m.procs[name]
  delete(m.procs, name)
  m.mu.Unlock()

  if proc != nil {
    proc.stop(ctx)
  }
}

// ready is a Client's Ready hook: a no-op while name runs, a respawn
// after the idle reaper (or a crash) stopped it.
func (m *Manager) ready(name string) func(ctx context.Context) error {
  return func(ctx context.Context) error {
    _, err := m.ensure(ctx, name)
    return err
  }
}

func (m *Manager) newClientFor(name, url string) *llm.Client {
  c := llm.New(url, name)
  c.ID, c.Effort = m.id(name)
  c.ReadTimeout = m.ReadTimeout
  c.APIKey = m.apiKey(name)
  c.OnRequestStart, c.OnRequestEnd = m.callbacks(name)
  c.Ready = m.ready(name)

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

// id is name's hosted-API model id and reasoning effort, if set.
func (m *Manager) id(name string) (id, effort string) {
  m.mu.Lock()
  defer m.mu.Unlock()

  return m.specs[name].ID, m.specs[name].Effort
}

func (m *Manager) callbacks(name string) (start, end func()) {
  return func() { m.touch(name, 1) }, func() { m.touch(name, -1) }
}

// ensure resolves name to a URL, spawning and health-polling its
// process first if it's a managed spec that isn't already running.
func (m *Manager) ensure(ctx context.Context, name string) (string, error) {
  m.adopt(name)
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

  proc, running := m.procs[name]
  if running {
    if !proc.exited() {
      proc.lastUsed = time.Now()
      m.mu.Unlock()

      return spec.url(), nil
    }

    // Died under us (crash, dropped SSH): forget it and respawn. An
    // instance armin started stays armin's to stop (see owned).
    delete(m.procs, name)
    if proc.remote != nil && proc.remote.started {
      m.owned[proc.remote.machine.ID()] = proc.remote
    }
  }

  m.mu.Unlock()

  err := m.spawn(ctx, name, spec)
  if err != nil {
    return "", err
  }

  return spec.url(), nil
}

func (m *Manager) spawn(ctx context.Context, name string, spec ModelSpec) error {
  m.mu.Lock()
  spec = spec.inTier(m.tiers[name])
  m.mu.Unlock()

  if spec.Remote != nil {
    return m.spawnRemote(ctx, name, spec)
  }

  args := append(
    []string{ "-m", spec.ModelPath, "--port", fmt.Sprint(spec.Port) },
    serverArgs(spec)...,
  )
  cmd := exec.Command(spec.Binary, args...)
  // Independent of ctx on purpose: this process must outlive
  // whatever single request triggered spawning it. Its lifetime is
  // owned entirely by the reaper and Shutdown below.
  cmd.Env = m.scrubbedEnviron()
  proc, err := startProcess(cmd)
  if err != nil {
    return fmt.Errorf(
      "\n\t... in spawn:\n\t starting %q for %q: %w",
      spec.Binary, name, err,
    )
  }

  proc.lastUsed = time.Now()

  m.mu.Lock()
  m.procs[name] = proc
  m.mu.Unlock()

  err = waitHealthy(ctx, spec.url(), proc, spawnHealthTimeout)
  if err != nil {
    m.mu.Lock()
    delete(m.procs, name)
    m.mu.Unlock()
    proc.stop(ctx)

    return fmt.Errorf(
      "\n\t... in spawn:\n\t %q never became healthy: %w\n\t llama-server said: %s",
      name, err, lastLines(proc.out.String(), 4),
    )
  }

  return nil
}

// leanDefaults spend VRAM (and host RAM) on context instead of on
// things a single-user, text-only agent doesn't use. Each applies
// only when the user's args set none of its flags.
var leanDefaults = []struct {
  flags, args []string
}{
  // The vision encoder is a separate projector file llama.cpp loads
  // only with --mmproj, or by itself when -hf pulls a model that
  // ships one (~0.9 GB for a 27B Qwen).
  {
    []string{
      "--no-mmproj", "--no-mmproj-auto", "--mmproj-auto",
      "-mm", "--mmproj", "-mmu", "--mmproj-url",
    },
    []string{ "--no-mmproj" },
  },
  // One slot: the server's auto default is 4, each with its own
  // sequence state, for parallel users armin doesn't have.
  { []string{ "-np", "--parallel" }, []string{ "-np", "1" } },
  // Flash attention: less scratch memory, and required for a
  // quantized V cache.
  { []string{ "-fa", "--flash-attn" }, []string{ "-fa", "on" } },
  // q8_0 KV cache: half of f16, with negligible quality loss.
  { []string{ "-ctk", "--cache-type-k" }, []string{ "-ctk", "q8_0" } },
  { []string{ "-ctv", "--cache-type-v" }, []string{ "-ctv", "q8_0" } },
  // Host-RAM prompt cache: 8 GiB by default, which a laptop needs
  // for itself; one slot gains little from it.
  { []string{ "-cram", "--cache-ram" }, []string{ "-cram", "1024" } },
}

// serverArgs is spec.Args plus every lean default the args don't
// already decide (the projector one is skipped with SpecVision).
func serverArgs(spec ModelSpec) []string {
  args := append([]string{}, spec.Args...)
  if spec.Flags & SpecNoLean != 0 {
    return args
  }

  set := make(map[string]bool, len(args))
  for _, a := range args {
    name, _, _ := strings.Cut(a, "=")
    set[name] = true
  }

  for i, d := range leanDefaults {
    if i == 0 && spec.Flags & SpecVision != 0 {
      continue
    }

    decided := false
    for _, f := range d.flags {
      if set[f] {
        decided = true
        break
      }
    }

    if !decided {
      args = append(args, d.args...)
    }
  }

  return args
}

// scrubbedEnviron is os.Environ() with every configured
// models.<name>.api_key_env name removed. exec.Cmd with a nil Env
// inherits the parent's environment in full, which would otherwise
// hand a Claude (or any other remote model's) API key to a local
// llama-server process that has no business seeing it.
// Environ is armin's environment without any API key or cloud
// credential, for every other process armin starts (builds, tests,
// programs the model runs).
func (m *Manager) Environ() []string {
  return m.scrubbedEnviron()
}

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

// waitHealthy polls url until llama-server answers, giving up early
// if the process behind it dies.
func waitHealthy(
  ctx context.Context, url string, proc *process, timeout time.Duration,
) error {
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()

  client := llm.New(url, "")
  for {
    err := client.Health(ctx)
    if err == nil {
      return nil
    }

    select {
    case <-ctx.Done():
      return fmt.Errorf("timed out waiting for llama-server to come up")
    case <-proc.done:
      return fmt.Errorf("the process exited before llama-server came up")
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
  proc, ok := m.procs[name]
  if ok {
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
  var toStop []*process
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

    toStop = append(toStop, proc)
    delete(m.procs, name)
  }

  m.mu.Unlock()

  for _, p := range toStop {
    p.stop(context.Background())
  }
}

// Shutdown stops the reaper and every managed process this Manager
// spawned — and every EC2 instance it started — but never touches an
// externally managed (URL-only) spec or an instance that was already
// running. Safe to call more than once (a signal handler may race
// the normal exit path).
func (m *Manager) Shutdown(ctx context.Context) error {
  m.shutdown.Do(func() {
    close(m.stopReap)
    <-m.reapDone
  })

  m.mu.Lock()
  procs, owned := m.procs, m.owned
  m.procs = make(map[string]*process)
  m.owned = make(map[string]*remoteRun)
  m.mu.Unlock()

  var wg sync.WaitGroup
  for _, proc := range procs {
    wg.Add(1)
    go func() {
      defer wg.Done()
      proc.stop(ctx)
    }()
  }

  for _, run := range owned {
    wg.Add(1)
    go func() {
      defer wg.Done()
      run.stopMachine()
    }()
  }

  wg.Wait()

  return nil
}

// stop sends SIGTERM and waits briefly for a clean exit, falling
// back to SIGKILL; then stops the EC2 instance if armin started it.
func (p *process) stop(ctx context.Context) {
  if p.cmd.Process != nil && p.done != nil {
    p.cmd.Process.Signal(syscall.SIGTERM)
    select {
    case <-p.done:
    case <-time.After(stopGraceTimeout):
      p.cmd.Process.Kill()
      <-p.done
    }
  }

  if p.remote != nil && p.remote.started {
    p.remote.stopMachine()
  }
}

// stopMachine gets its own deadline: it must run even when the
// caller's context is the one being cancelled.
func (r *remoteRun) stopMachine() {
  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  r.machine.Stop(ctx)
}

// SetTier restarts name's process in tier t, unless it's already
// there. The URL doesn't change, so every Client keeps working.
func (m *Manager) SetTier(ctx context.Context, name string, t Tier) error {
  m.mu.Lock()
  spec, ok := m.specs[name]
  if !ok || spec.Stream == nil {
    m.mu.Unlock()
    return fmt.Errorf("\n\t... in SetTier:\n\t model %q has no stream tier", name)
  }

  cur := m.tiers[name]
  if cur == 0 {
    cur = TierResident
  }

  if cur == t {
    m.mu.Unlock()
    return nil
  }

  proc := m.procs[name]
  delete(m.procs, name)
  m.tiers[name] = t
  m.mu.Unlock()

  if proc != nil {
    // Keep a remote machine: only the server on it restarts.
    remote := proc.remote
    proc.remote = nil
    proc.stop(ctx)
    if remote != nil && remote.started {
      m.mu.Lock()
      m.owned[remote.machine.ID()] = remote
      m.mu.Unlock()
    }
  }

  _, err := m.ensure(ctx, name)
  if err != nil {
    return fmt.Errorf("\n\t... in SetTier:\n\t%w", err)
  }

  return nil
}

// Window returns name's tier control, or nil when the model has no
// stream tier or no explicit -c to size the resident one.
func (m *Manager) Window(name string) *Window {
  m.mu.Lock()
  defer m.mu.Unlock()
  spec, ok := m.specs[name]
  if !ok || spec.Stream == nil {
    return nil
  }

  resident := contextOf(spec.Args)
  if resident == 0 || spec.Stream.Context <= resident {
    return nil
  }

  return &Window{ m: m, name: name, resident: resident, max: spec.Stream.Context }
}

func (w *Window) Resident() int { return w.resident }
func (w *Window) Max() int      { return w.max }

// Streaming reports whether the large tier is active.
func (w *Window) Streaming() bool {
  w.m.mu.Lock()
  defer w.m.mu.Unlock()

  return w.m.tiers[w.name] == TierStream
}

// Size is the active tier's window.
func (w *Window) Size() int {
  if w.Streaming() {
    return w.max
  }

  return w.resident
}

// Grow moves to the stream tier and returns its window.
func (w *Window) Grow(ctx context.Context) (int, error) {
  err := w.m.SetTier(ctx, w.name, TierStream)
  if err != nil {
    return 0, err
  }

  return w.max, nil
}

// Shrink moves back to the resident tier and returns its window.
func (w *Window) Shrink(ctx context.Context) (int, error) {
  err := w.m.SetTier(ctx, w.name, TierResident)
  if err != nil {
    return 0, err
  }

  return w.resident, nil
}
