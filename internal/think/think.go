// Package think is armin's mind: the sandbox its tools act in, the
// models it reasons with, the agent loop tying them together, and
// what it remembers. Package display decides how it's shown.
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

  m := &Mind{ Sandbox: sb }

  // web_search degrades gracefully: without docker the tool reports
  // "unavailable". The container starts on the first search.
  _, err = exec.LookPath("docker")
  if err == nil {
    m.WebSearch = searxng.New()
  }

  // One ctags index for the agent's symbol tools, -lsp's definition
  // and workspace/symbol, and -mcp — the repo is scanned once.
  m.Symbols = ctags.New(sb.Root, opts.CtagsBin)

  // The reference library: its folder is readable like the project,
  // so read_pdf and read_file can follow a citation.
  if cfg.Library != "" {
    lib, err := library.Open(cfg.Library)
    if err != nil {
      ui.Warn("library: %s", err)
    } else {
      m.Library = lib
      sb.AddReadRoot(lib.Root)
    }
  }

  return m, nil
}

// wake starts the models and the agent loop.
func (m *Mind) wake(ctx context.Context, opts *input.Options) error {
  cfg := opts.Config

  // models owns every configured llama-server: spawning one on first
  // use, reaping it when idle, never touching one the user runs.
  m.Models = llamacpp.New(buildModelSpecs(opts))
  m.Sandbox.Env = m.Models.Environ // no API keys for builds, tests or programs
  m.Models.ReadTimeout = cfg.ReadTimeout
  // stderr: -lsp speaks its protocol on stdout.
  m.Models.OnStatus = func(s string) {
    fmt.Fprintln(os.Stderr, ui.Dim("  " + s))
  }

  // Before the chat model loads: on the CPU that takes minutes.
  if m.Models.FirstLocal() != "" {
    m.Models.Warning = llamacpp.GPUWarning()
    if m.Models.Warning != "" {
      ui.Warn("%s", m.Models.Warning)
    }
  }

  data, err := session.DataDir()
  if err == nil {
    os.MkdirAll(data, 0o700)
    m.Models.KnownHostsFile = filepath.Join(data, "remote_known_hosts")
  }

  client, err := m.Models.Client(ctx, opts.Model)
  if err != nil {
    return fmt.Errorf("\n\t... in wake:\n\t starting model %q: %w", opts.Model, err)
  }

  m.Client = client

  // Model suggestions for /model and the pickers, fetched while the
  // user gets going.
  hosted := m.Models.OpenRouter()
  if hosted != "" {
    catalog.Warm()
    catalog.CachedCredits(m.Models.Endpoint(hosted))
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
    return fmt.Errorf("\n\t... in wake:\n\t%w", err)
  }

  loop := agent.New(client, m.Sandbox)
  loop.WebSearch = m.WebSearch
  loop.Symbols = m.Symbols
  loop.MaxRounds = opts.MaxRounds
  loop.KeepTurns = cfg.KeepTurns
  loop.ContextSize = cfg.ContextSize
  if loop.ContextSize == 0 {
    loop.ContextSize = queryContextSize(ctx, client)
  }

  // A model with a stream tier moves between its fast resident
  // window and the large streamed one.
  w := m.Models.Window(opts.Model)
  if w != nil {
    loop.Window = w
  }

  m.Memories, m.Skills, m.Scratch, err = openNotes(m.Sandbox.Root)
  if err != nil {
    return fmt.Errorf("\n\t... in wake:\n\t%w", err)
  }

  loop.Memories, loop.Skills, loop.Scratch = m.Memories, m.Skills, m.Scratch
  m.Loop = loop

  loop.Aids = opts.Aids
  confDir, err := config.Dir()
  if err == nil {
    loop.Procedures = agent.LoadProcedures(filepath.Join(confDir, "procedures"))
  }

  if m.Library != nil {
    loop.Library = m.Library
    if opts.Mode != input.ModeLibrary {
      m.refreshLibrary(ctx)
    }
  }

  // A resumed session continues on the model it used.
  r := opts.Resumed
  if r != nil && r.Model != "" && r.Model != client.Model && m.Models.Has(r.Model) {
    err := m.Models.SwitchChat(ctx, client, m.Follower(), r.Model)
    if err != nil {
      ui.Error("resuming with model %q: %s (staying on %q)", r.Model, err, opts.Model)
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
    base, m.Sandbox.Root, m.Memories, m.Skills, m.Library,
  )
}

// refreshLibrary reindexes a library that changed since its index
// was built, in the background (searches use the old index
// meanwhile), and points at books the topic table lacks.
func (m *Mind) refreshLibrary(ctx context.Context) {
  lib := m.Library
  pending := len(lib.Pending(nil))
  if pending > 0 {
    msg := fmt.Sprintf("  library: %d books aren't in the topic "+
      "table yet — /library catalog", pending)
    fmt.Fprintln(os.Stderr, ui.Dim(msg))
  }

  if !lib.Stale() {
    return
  }

  go func() {
    err := lib.Build(ctx, nil)
    if err != nil && ctx.Err() == nil {
      msg := "  library: indexing failed: " + err.Error()
      fmt.Fprintln(os.Stderr, ui.Dim(msg))
    }
  }()
}

// Shutdown stops only what armin itself started. Safe to call twice.
func (m *Mind) Shutdown() {
  ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
  defer cancel()

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
