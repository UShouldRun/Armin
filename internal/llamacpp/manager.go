// Package llamacpp runs the models: it starts llama-server processes
// (on this machine or on a cloud GPU it boots), stops the ones it
// started when they sit idle, and hands out llm.Clients pointed at
// whichever URL serves a named model. llama-server loads one model
// per process, so switching models means pointing at another URL,
// starting its server if needed.
//
// A ModelSpec (config.conf's models.<name>) is either external, just
// a URL (a hosted API, a server the user runs): never started or
// stopped here; or managed, a binary, a .gguf and a port: started on
// first use, stopped after its idle timeout, restarted on the next.
//
//   manager.go  the Manager: Client, Switch, LazyClient (names to
//               clients), ensure (start on demand), the idle reaper,
//               Shutdown
//   spec.go     what a ModelSpec means, the server's arguments
//   process.go  starting, health-checking and stopping a server
//   remote.go   a server on a cloud machine, over SSH
//   tier.go     resident and streamed context tiers
//   gpu.go      warning when the NVIDIA driver can't be used
//
// think builds the Manager from config.conf (think/models.go).
package llamacpp

import (
  "context"
  "fmt"
  "os"
  "strings"
  "sync"
  "time"

  "armin/internal/catalog"
  "armin/internal/cloud"
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

const (
  // SpecVision keeps a multimodal projector (--mmproj) loadable.
  SpecVision SpecFlags = 1 << iota
  // SpecNoLean drops every default in leanDefaults.
  SpecNoLean
  // SpecRegional marks a URL armin moved onto OpenRouter's in-region
  // host (catalog.Regional): one that falls back to the global host
  // when the region refuses, unlike a regional URL configured as such.
  SpecRegional
  SpecAll = SpecVision | SpecNoLean
)

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
  for _, name := range cloud.CredentialEnv {
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
  client.Fallback, client.OnFallback = m.globalFor(name, url), m.fallback

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
  c.Fallback, c.OnFallback = m.globalFor(name, url), m.fallback

  return c
}

// globalFor is where name's url falls back to when its region
// refuses: only for a URL armin made regional itself.
func (m *Manager) globalFor(name, url string) string {
  m.mu.Lock()
  regional := m.specs[name].Flags & SpecRegional != 0
  m.mu.Unlock()
  if !regional {
    return ""
  }

  return catalog.Global(url)
}

// fallback moves every model on from — an in-region host that
// refused — to to, the global one, so later Clients start there.
func (m *Manager) fallback(from, to string) {
  m.mu.Lock()
  for name, spec := range m.specs {
    if spec.URL == from {
      spec.URL = to
      m.specs[name] = spec
    }
  }
  m.mu.Unlock()

  m.status("%s refused (in-region routing needs a Business plan) — using %s", from, to)
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

