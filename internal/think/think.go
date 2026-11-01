// Package think is armin's mind: the sandbox its tools act in, the
// models it reasons with, the agent loop tying them together, and
// what it remembers. It's main's second step (input → think →
// display): New turns input's Options into a Mind, and display shows
// it.
//
//   think.go   New: the sandbox, web search, symbols and library
//              (NewSenses), then the models and the agent loop
//              (wake → startModels, buildLoop); Shutdown stops what
//              armin started
//   models.go  config.conf's models.* entries → llamacpp specs
//   prompt.go  the system prompts and what's appended to them
//              (project instructions, memories, skills, library)
//
// Everything a frontend needs is on Mind: the Loop to run turns, the
// Client and Models to switch models, the stores for slash commands.
package think

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "time"

  "armin/internal/agent"
  "armin/internal/catalog"
  "armin/internal/config"
  "armin/internal/ctags"
  "armin/internal/input"
  "armin/internal/library"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/metrics"
  "armin/internal/repomap"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/session"
  "armin/internal/ui"
)

// shutdownTimeout bounds stopping what armin started: llama-server
// processes, cloud machines, the SearXNG container.
const shutdownTimeout = 15 * time.Second

// New builds the mind opts asks for. Models start here (the chat
// one spawned now), so Shutdown must run whatever happens next.
func New(ctx context.Context, opts *input.Options) (*Mind, error) {
  m, err := NewSenses(opts)
  if err != nil || opts.Mode == input.ModeMCP {
    return m, err
  }

  err = m.wake(ctx, opts)
  if err != nil {
    m.Shutdown()
    return nil, err
  }

  return m, nil
}

// NewSenses is the mind without models: the sandbox, web search and
// the symbol index. -mcp needs no more, nor an nvim joining another
// armin's instance.
func NewSenses(opts *input.Options) (*Mind, error) {
  cfg := opts.Config

  allowlist := sandbox.DefaultAllowlist()
  for _, bin := range cfg.ExtraAllowed {
    allowlist[bin] = true
  }

  execAllowlist := sandbox.DefaultExecAllowlist()
  for _, bin := range cfg.ExtraExec {
    execAllowlist[bin] = true
  }

  sb, err := sandbox.New(opts.Root, sandbox.Options{
    ExecAllowlist: execAllowlist,
    Timeout:       opts.Timeout,
    BuildTimeout:  cfg.BuildTimeout,
    MaxOutput:     opts.MaxOutput,
    Allowlist:     allowlist,
    DryRun:        opts.Has(input.FlagDryRun),
    Commands: sandbox.Commands{
      Test:  cfg.TestCmd,
      Build: cfg.BuildCmd,
      Lint:  cfg.LintCmd,
    },
  })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in New:\n\t setting up sandbox: %w", err)
  }

  m := &Mind{ Sandbox: sb, Notices: make(chan string, 16) }

  // what the deterministic systems did, kept with armin's data
  data, err := session.DataDir()
  if err == nil {
    metrics.SetDir(filepath.Join(data, "metrics"))
  }

  // web_search degrades gracefully: without docker the tool reports
  // "unavailable". The container starts on the first search.
  _, err = exec.LookPath("docker")
  if err == nil {
    m.WebSearch = searxng.New()
  }

  // One ctags index for the agent's symbol tools, -lsp's definition
  // and workspace/symbol, and -mcp — the repo is scanned once.
  t0 := time.Now()
  m.Symbols = ctags.New(sb.Root, opts.CtagsBin)
  m.took("senses.symbols", t0)
  // the map is for the system prompt, which -mcp never builds
  if !cfg.NoRepoMap && opts.CtagsBin != "" && opts.Mode != input.ModeMCP {
    m.startRepoMap()
  }

  // The reference library: its folder is readable like the project,
  // so read_pdf and read_file can follow a citation.
  if cfg.Library != "" {
    t0 := time.Now()
    lib, err := library.Open(cfg.Library)
    m.took("senses.library", t0)
    if err != nil {
      ui.Warn("library: %s", err)
    } else {
      m.Library = lib
      sb.AddReadRoot(lib.Root)
    }
  }

  return m, nil
}

// wake starts the models, then builds the agent loop on the chat one.
func (m *Mind) wake(ctx context.Context, opts *input.Options) error {
  err := m.startModels(ctx, opts)
  if err != nil {
    return fmt.Errorf("\n\t... in wake:\n\t%w", err)
  }

  err = m.buildLoop(ctx, opts)
  if err != nil {
    return fmt.Errorf("\n\t... in wake:\n\t%w", err)
  }

  return nil
}

// startModels sets up the model manager and starts the chat model
// (loading a local one now, on the GPU if there is one); inline
// completion gets a client that loads on first use.
func (m *Mind) startModels(ctx context.Context, opts *input.Options) error {
  cfg := opts.Config

  // models owns every configured llama-server: spawning one on first
  // use, reaping it when idle, never touching one the user runs.
  m.Models = llamacpp.New(buildModelSpecs(opts))
  m.Sandbox.Env = m.Models.Environ // no API keys for builds, tests or programs
  m.Models.ReadTimeout = cfg.ReadTimeout
  // stderr: -lsp speaks its protocol on stdout. The leading blank
  // line keeps a mid-request notice (a refused regional host) from
  // splicing into the answer text or the prompt above it.
  m.Models.OnStatus = func(s string) {
    fmt.Fprintln(os.Stderr, "\n" + ui.Dim("  " + s))
  }

  // Before the chat model loads: on the CPU that takes minutes.
  if m.Models.FirstLocal() != "" {
    t0 := time.Now()
    m.Models.Warning = llamacpp.GPUWarning()
    m.took("models.gpu-check", t0)
    if m.Models.Warning != "" {
      ui.Warn("%s", m.Models.Warning)
    }
  }

  data, err := session.DataDir()
  if err == nil {
    os.MkdirAll(data, 0o700)
    m.Models.KnownHostsFile = filepath.Join(data, "remote_known_hosts")
  }

  // The chat model: a resumed session's, when it used another, so
  // the default one isn't loaded for nothing (its load is background;
  // dropping it saves the GPU the switch).
  chat := opts.Model
  r := opts.Resumed
  if r != nil && r.Model != "" && m.Models.Has(r.Model) {
    chat = r.Model
  }

  client, err := m.chatClient(ctx, chat)
  if err != nil {
    return fmt.Errorf(
      "\n\t... in startModels:\n\t starting model %q: %w", chat, err,
    )
  }

  m.Client = client

  // Model suggestions for /model and the pickers, fetched while the
  // user gets going.
  hosted := m.Models.OpenRouter()
  if hosted != "" {
    t0 := time.Now()
    catalog.Warm()
    catalog.CachedCredits(m.Models.Endpoint(hosted))
    m.took("models.catalog", t0)
  }

  // Lazy: an explicit completion_model loads on its first use, not
  // beside the chat model at startup.
  // Without its own completion_model, completion follows chat
  // between local models — and when chat starts on a hosted one, it
  // starts on a local model instead of billing each keystroke.
  completion := opts.CompletionModel
  m.follows = opts.Config.CompletionModel == ""
  if m.follows && !m.Models.IsLocal(completion) {
    local := m.Models.FirstLocal()
    if local != "" {
      completion = local
    }
  }

  m.Completion, err = m.Models.LazyClient(completion)
  if err != nil {
    return fmt.Errorf("\n\t... in startModels:\n\t%w", err)
  }

  return nil
}

// buildLoop wires the agent loop to the chat model and to everything
// a turn uses: the sandbox's services, notes, reasoning aids, the
// library. A resumed session goes back to the model it used.
func (m *Mind) buildLoop(ctx context.Context, opts *input.Options) error {
  cfg := opts.Config
  client := m.Client
  loop := agent.New(client, m.Sandbox)
  loop.WebSearch = m.WebSearch
  loop.Symbols = m.Symbols
  loop.MaxRounds = opts.MaxRounds
  loop.KeepTurns = cfg.KeepTurns
  loop.ContextSize = cfg.ContextSize
  if loop.ContextSize == 0 {
    loop.ContextSize = m.Models.ContextOf(opts.Model)
  }

  // asking the server: only when its args don't say, since the chat
  // model may still be loading
  if loop.ContextSize == 0 {
    t0 := time.Now()
    loop.ContextSize = queryContextSize(ctx, client)
    m.took("loop.context-size", t0)
  }

  // A model with a stream tier moves between its fast resident
  // window and the large streamed one.
  w := m.Models.Window(opts.Model)
  if w != nil {
    loop.Window = w
  }

  var err error
  t0 := time.Now()
  m.Memories, m.Skills, m.Scratch, err = openNotes(m.Sandbox.Root)
  m.took("loop.notes", t0)
  if err != nil {
    return fmt.Errorf("\n\t... in buildLoop:\n\t%w", err)
  }

  loop.Memories, loop.Skills, loop.Scratch = m.Memories, m.Skills, m.Scratch
  m.Loop = loop

  // What the user knows: one learner tree per user, for the advisor.
  t0 = time.Now()
  m.Learner, err = openLearner()
  m.took("loop.learner", t0)
  if err != nil {
    ui.Warn("learner tree: %s", err)
  } else {
    loop.Learner = m.Learner
  }

  // The minion: delegate's model, and the helper calls' while chat
  // runs on a hosted one. Started on first use.
  minion := cfg.Minion
  if minion != "" && m.Models.Has(minion) {
    loop.Minion = func(ctx context.Context) (*llm.Client, error) {
      return m.Models.Client(ctx, minion)
    }
  }

  // The project's knowledge DAG; without one, the know tools are
  // simply not offered.
  t0 = time.Now()
  m.Know, err = openKnow(m.Sandbox.Root)
  m.took("loop.know-open", t0)
  if err != nil {
    ui.Warn("knowledge DAG: %s", err)
  } else {
    loop.Know = m.Know
    loop.Recall = !cfg.NoRecall
    t0 = time.Now()
    msg, err := refreshKnow(m.Sandbox.Root, m.Know)
    m.took("loop.know-refresh", t0)
    if err != nil {
      m.notice("knowledge DAG: " + err.Error())
    } else if msg != "" {
      m.notice(msg)
    }
  }

  // with the minion and a refreshed tree in place: consolidate in the
  // background
  m.startConsolidation()

  loop.Aids = opts.Aids
  confDir, err := config.Dir()
  if err == nil {
    dir := filepath.Join(confDir, "procedures")
    loop.Procedures = agent.LoadProcedures(dir)
  }

  if m.Library != nil {
    loop.Library = m.Library
    if opts.Mode != input.ModeLibrary {
      go m.refreshLibrary(ctx) // a scan of the library: off the start
    }
  }

  // A resumed session continues on the model it used: chat starts
  // there, not on the default one (whose load a resume would waste),
  // and the window and context size describe the model the header
  // actually shows.
  r := opts.Resumed
  if r != nil && r.Model != "" && r.Model != client.Model && m.Models.Has(r.Model) {
    loop.Window = nil
    w := m.Models.Window(r.Model)
    if w != nil {
      loop.Window = w
    }

    loop.ContextSize = cfg.ContextSize
    if loop.ContextSize == 0 {
      loop.ContextSize = m.Models.ContextOf(r.Model)
    }

    err := m.Models.SwitchChat(ctx, client, m.Follower(), r.Model)
    if err != nil {
      ui.Error("resuming with model %q: %s (staying on %q)",
        r.Model, err, opts.Model)
    }
  }

  return nil
}

// Follower is Completion when it follows chat between local models,
// nil when completion_model pins it.
func (m *Mind) Follower() *llm.Client {
  if !m.follows {
    return nil
  }

  return m.Completion
}

// SystemPrompt is the base prompt for a frontend that can edit files
// (the REPL, the web chat) or only read (nvim), plus the project's
// instructions and the memory and skill indexes.
func (m *Mind) SystemPrompt(readOnly bool) string {
  base := basePrompt
  if readOnly {
    base = readOnlyPrompt
  }

  return buildSystemPrompt(
    base, m.Sandbox.Root, m.Memories, m.Skills, m.Library, m.Know,
    m.Loop != nil && m.Loop.Recall,
  ) + mapSection(m.RepoMap())
}

// mapBytes is the repo map's budget in the prompt, about 2k tokens:
// aider's default map is 1k, and ours has no chat files to focus it.
const mapBytes = 8000

// startRepoMap builds the repo map in the background, so the ctags
// scan overlaps the rest of starting.
func (m *Mind) startRepoMap() {
  m.mapReady = make(chan struct{})
  go func() {
    defer close(m.mapReady)

    t0 := time.Now()
    m.repoMap = repomap.Build(context.Background(), m.Symbols, mapBytes)
    metrics.Since("startup.repo-map.ms", t0)
  }()
}

// RepoMap is the static repo map, waiting for it if it is still
// being built; "" with repo_map = off or without ctags.
func (m *Mind) RepoMap() string {
  if m.mapReady == nil {
    return ""
  }

  <-m.mapReady

  return m.repoMap
}

// refreshLibrary reindexes a library that changed since its index
// was built, in the background (searches use the old index
// meanwhile), and points at books the topic table lacks.
func (m *Mind) refreshLibrary(ctx context.Context) {
  lib := m.Library
  t0 := time.Now()
  pending := len(lib.Pending(nil))
  metrics.Since("startup.library-scan.ms", t0)
  if pending > 0 {
    m.notice(fmt.Sprintf("library: %d books aren't in the topic " +
      "table yet — /library catalog", pending))
  }

  if !lib.Stale() {
    return
  }

  go func() {
    err := lib.Build(ctx, nil)
    if err != nil && ctx.Err() == nil {
      m.notice("library: indexing failed: " + err.Error())
    }
  }()
}

// Shutdown stops only what armin itself started. Safe to call twice.
func (m *Mind) Shutdown() {
  ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
  defer cancel()

  m.stopConsolidation()
  m.closeKnow()
  err := metrics.Flush()
  if err != nil {
    ui.Error("saving metrics: %s", err)
  }

  if m.Models != nil {
    err := m.Models.Shutdown(ctx)
    if err != nil {
      ui.Error("stopping managed llama-server processes: %s", err)
    }
  }

  if m.WebSearch != nil {
    err := m.WebSearch.Shutdown(ctx)
    if err != nil {
      ui.Error("stopping the searxng container: %s", err)
    }
  }
}
