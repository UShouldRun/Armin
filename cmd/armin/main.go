// Command armin is a terminal chat assistant, backed by a local
// llama.cpp model, that can search/read a codebase and run
// whitelisted shell commands to inspect it. Run with -mcp to instead
// expose the same sandboxed tools as an MCP stdio server, or -lsp to
// expose hover + a single ask command over the Language Server
// Protocol (for editor integration — see docs/nvim.md).
//
// The project root can be given as a bare positional argument
// ("armin .", "armin ~/code/myproject") instead of -root, mirroring
// how tools like "code ." or "vim ." take a path.
package main

import (
  "context"
  "io"
  "flag"
  "fmt"
  "os"
  "os/exec"
  "os/signal"
  "path/filepath"
  "strconv"
  "strings"
  "sync/atomic"
  "syscall"
  "time"

  "armin/internal/agent"
  "armin/internal/config"
  "armin/internal/ctags"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/lspserver"
  "armin/internal/mcpserver"
  "armin/internal/notes"
  "armin/internal/repl"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/session"
  "armin/internal/ui"
  "armin/internal/web"
)

const basePrompt = `You are Armin, a coding assistant working in the user's project from their terminal.
You can search, read and edit files, run the project's build, tests and linter, look at git history, and search the web.

How to work:
- Ground every answer in what the tools return; never guess at file contents or project structure.
- Locate before reading: search_code / find_definition / outline_file, then read_file a narrow range.
- To change code: read the exact region, then edit_file with old_string copied exactly (whitespace included). Use write_file only for new files or full rewrites.
- After changing code, run build_check, and run_tests when tests exist. Fix what you broke before reporting back.
- For multi-step work, write the plan to the scratchpad (scratchpad_write "plan") and tick items off as you go; put long findings there too. Keep only names and pointers in mind, and re-read when needed — your context is limited.
- Old tool results get replaced by stubs; call the tool again if you need one back.
- Save durable facts (user preferences, project conventions, decisions) with memory_save; not task progress.
- If a skill in the index covers the task, load it with use_skill first.
- If web_search reports an error or finds nothing relevant, tell the user so plainly. Never present what you write from memory as researched or sourced.
- When you used web_search in this turn, cite every claim that comes from a result inline as [id], using the result's id, and end the answer with a Sources list: "- [title](url)" for each id you cited. If you didn't search, don't add a Sources section; cite files as file:line instead.
- Your reasoning from earlier turns is saved in the scratchpad file "thinking"; old tool results you see as stubs are archived there too — read them back instead of re-running the tool.
- If the user declines a change, read their feedback and adjust; don't retry the same change.

How tools run:
- run_command runs ONE allowlisted program directly, with no shell. Pipes (|), redirects (>, <), &&, ;, globs (*.go), $VARS and cd do not work. Put the program in "binary" and each argument as its own array element: {"binary":"grep","args":["-rn","TODO","internal"]}, never {"binary":"grep -rn TODO | head"}.
- The working directory is always the project root. Use paths relative to it, or absolute paths; there is no cd. Need several steps? Make several calls.
- sed only prints (no -i); edit files with edit_file. find can't -exec or -delete.
- Prefer the dedicated tools: read_file over cat/head/tail, search_code over grep, read_pdf for PDFs (cat or pdftotext through run_command won't work).
- Files outside the project are off limits until granted. If you need a directory outside it (e.g. ".." or ~/Documents), call request_access with the directory and a one-line reason; the user approves or declines. Don't guess around the sandbox with other paths or commands.
- Lines starting with "/" (like /add-dir, /model) are the user's REPL commands, not tools; you can't run them. Suggest one when it would help.
- If a tool keeps failing, don't repeat it. After two failed attempts at the same thing, stop and tell the user exactly what failed and what you need.

Keep answers concise; this is a terminal, not a document.`

const readOnlyPrompt = `You are Armin, a coding assistant answering questions about the user's project inside their editor.
You can search and read files and look at git history, but not change anything.
Ground every answer in what the tools return; don't guess at file contents or project structure.
Locate before reading: search_code / find_definition, then read_file a narrow range.
If a skill in the index covers the question, load it with use_skill; memories hold the user's preferences and project facts.
When you used web_search, cite each claim from a result inline as [id] and end with a Sources list: "- [title](url)" per id cited.
Keep answers concise.`

// buildSystemPrompt appends the project's instruction file and the
// memory and skill indexes — names and one-liners only; the bodies
// are a tool call away.
func buildSystemPrompt(
  base, root string, mem *notes.Memories, skills *notes.Skills,
) string {
  var b strings.Builder
  b.WriteString(base)

  if name, body := notes.ProjectInstructions(root); body != "" {
    fmt.Fprintf(&b, "\n\n# Project instructions (%s)\n%s", name, body)
  }

  if index := mem.Index(); index != "" {
    b.WriteString("\n\n# Memories (memory_read for details)\n")
    b.WriteString(index)
  }

  if skills.Len() > 0 {
    b.WriteString("\n\n# Skills (use_skill to load)\n")
    b.WriteString(skills.Index())
  }

  return b.String()
}

func main() {
  if err := run(); err != nil {
    ui.Error("%s", err)
    os.Exit(1)
  }
}

func run() error {
  // Before config.Load(), so a models.<name>.api_key_env entry can
  // resolve against a key .env only just populated into the process
  // environment — see config.LoadEnv's doc comment.
  if err := config.LoadEnv(); err != nil {
    return fmt.Errorf("\n\t... in run:\n\t loading .env: %w", err)
  }

  fileCfg, err := config.Load()
  if err != nil {
    return fmt.Errorf("\n\t... in run:\n\t loading config: %w", err)
  }

  flags := parseFlags(fileCfg)

  // Picked before the sandbox exists: a session from another
  // directory resumes there.
  var resumed *session.Saved
  switch flags.resume {
  case "":
  case "menu":
    resumed, err = pickSession(flags.root)
  default:
    resumed, err = session.Load(flags.resume)
  }

  if err != nil {
    return fmt.Errorf("\n\t... in run:\n\t resuming: %w", err)
  }

  if resumed != nil && resumed.Root != "" {
    flags.root = resumed.Root
  }

  if flags.noColor {
    ui.Enabled = false
  }

  allowlist := sandbox.DefaultAllowlist()
  for _, bin := range fileCfg.ExtraAllowed {
    allowlist[bin] = true
  }

  sb, err := sandbox.New(flags.root, sandbox.Options{
    Timeout:      flags.timeout,
    BuildTimeout: fileCfg.BuildTimeout,
    MaxOutput:    flags.maxOutput,
    Allowlist:    allowlist,
    DryRun:       flags.dryRun,
    Commands: sandbox.Commands{
      Test:  fileCfg.TestCmd,
      Build: fileCfg.BuildCmd,
      Lint:  fileCfg.LintCmd,
    },
  })
  if err != nil {
    return fmt.Errorf(
      "\n\t... in run:\n\t setting up sandbox: %w", err,
    )
  }

  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  // SIGTERM / SIGHUP (kill, closed terminal) skip the REPL's normal
  // exit path, so they shut models down here — an EC2 instance armin
  // started must not outlive it.
  var terminate atomic.Pointer[func()]
  term := make(chan os.Signal, 1)
  signal.Notify(term, syscall.SIGTERM, syscall.SIGHUP)
  go func() {
    <-term
    cancel()
    if f := terminate.Load(); f != nil {
      (*f)()
    }

    os.Exit(1)
  }()

  // Ctrl-C cancels the REPL's turn in flight if there is one, and
  // only otherwise everything.
  var interrupt atomic.Pointer[func() bool]
  stop := make(chan os.Signal, 1)
  signal.Notify(stop, os.Interrupt)
  go func() {
    for range stop {
      if f := interrupt.Load(); f != nil && (*f)() {
        continue
      }

      cancel()

      return
    }
  }()

  // web_search degrades gracefully, same as -ctags-bin: no docker on
  // PATH just means the tool is still advertised but every call
  // reports a clean "unavailable" error instead of armin refusing to
  // start. The container itself isn't touched until the first
  // search; this shutdown runs on every exit path (including
  // mcpMode's early return) and only stops a container this process
  // itself started, never one the user already had running.
  var webSearch *searxng.Manager
  if _, err := exec.LookPath("docker"); err == nil {
    webSearch = searxng.New()
    defer func() {
      shutdownCtx, cancel := context.WithTimeout(
        context.Background(), 15 * time.Second,
      )
      defer cancel()
      if err := webSearch.Shutdown(shutdownCtx); err != nil {
        ui.Error("stopping the searxng container: %s", err)
      }
    }()
  }

  // One shared ctags index backs loop.Symbols (find_definition/
  // list_symbols/outline_file/find_dead_code/read_doc_comment),
  // -lsp's own textDocument/definition and workspace/symbol, and
  // -mcp's tools — so -lsp doesn't scan the repo with ctags twice.
  // Empty ctagsBin degrades gracefully everywhere it's used, same
  // as -ctags-bin's existing doc comment says.
  symbols := ctags.New(sb.Root, flags.ctagsBin)

  if flags.mcpMode {
    return mcpserver.Serve(ctx, sb, webSearch, symbols)
  }

  // models owns every configured llama-server: spawning/health-
  // polling a managed one on first use, reaping it after it sits
  // idle, and never touching one the user already runs themselves
  // (see internal/llamacpp). This shutdown, like webSearch's above,
  // only stops what armin itself started.
  models := llamacpp.New(buildModelSpecs(fileCfg, flags))
  models.ReadTimeout = fileCfg.ReadTimeout
  // stderr, not stdout: -lsp speaks its protocol on stdout.
  models.OnStatus = func(s string) {
    fmt.Fprintln(os.Stderr, ui.Dim("  " + s))
  }

  if data, err := session.DataDir(); err == nil {
    os.MkdirAll(data, 0o700)
    models.KnownHostsFile = filepath.Join(data, "remote_known_hosts")
  }

  onTerminate := func() {
    shutdownCtx, cancel := context.WithTimeout(
      context.Background(), 15 * time.Second,
    )
    defer cancel()
    models.Shutdown(shutdownCtx)
  }

  terminate.Store(&onTerminate)
  defer func() {
    shutdownCtx, cancel := context.WithTimeout(
      context.Background(), 15 * time.Second,
    )
    defer cancel()
    if err := models.Shutdown(shutdownCtx); err != nil {
      ui.Error("stopping managed llama-server processes: %s", err)
    }
  }()

  client, err := models.Client(ctx, flags.model)
  if err != nil {
    return fmt.Errorf(
      "\n\t... in run:\n\t starting model %q: %w", flags.model, err,
    )
  }

  loop := agent.New(client, sb)
  loop.WebSearch = webSearch
  loop.Symbols = symbols
  loop.MaxRounds = flags.maxRounds
  loop.KeepTurns = fileCfg.KeepTurns
  loop.ContextSize = fileCfg.ContextSize
  if loop.ContextSize == 0 {
    loop.ContextSize = queryContextSize(ctx, client)
  }

  // A model with a stream tier gets a window control instead: armin
  // moves between its fast resident window and the large streamed one.
  if w := models.Window(flags.model); w != nil {
    loop.Window = w
  }

  mem, skills, scratch, err := openNotes(sb.Root)
  if err != nil {
    return fmt.Errorf("\n\t... in run:\n\t%w", err)
  }

  loop.Memories, loop.Skills, loop.Scratch = mem, skills, scratch

  if flags.lspMode {
    loop.ReadOnly = true
    completionClient, err := models.Client(ctx, flags.completionModel)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in run:\n\t starting completion model %q: %w",
        flags.completionModel, err,
      )
    }

    server := lspserver.New(
      loop, sb, buildSystemPrompt(readOnlyPrompt, sb.Root, mem, skills),
      completionClient, models, symbols,
    )
    if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
      return fmt.Errorf("\n\t... in run:\n\t%w", err)
    }

    return nil
  }

  systemPrompt := buildSystemPrompt(basePrompt, sb.Root, mem, skills)

  if flags.webAddr != "" {
    return web.Serve(ctx, web.Config{
      Addr:         flags.webAddr,
      Loop:         loop,
      Client:       client,
      Models:       models,
      Sandbox:      sb,
      SystemPrompt: systemPrompt,
      AutoApprove:  flags.autoApprove || fileCfg.AutoApprove,
    })
  }

  if resumed != nil && resumed.Model != "" && resumed.Model != client.Model && models.Has(resumed.Model) {
    if err := models.Switch(ctx, client, resumed.Model); err != nil {
      ui.Error(
        "resuming with model %q: %s (staying on %q)",
        resumed.Model, err, flags.model,
      )
    }
  }

  r := repl.New(
    loop, client, sb, systemPrompt, resumed, !flags.noSave, models,
  )
  r.SetAutoApprove(flags.autoApprove || fileCfg.AutoApprove)
  r.ScratchBase = scratch.Dir
  r.BindScratch()
  for _, dir := range splitList(flags.addDirs) {
    if err := r.AddDir(dir); err != nil {
      ui.Error("-add-dir %s: %s", dir, err)
    }
  }

  r.ContextFromServer = fileCfg.ContextSize == 0
  if resumed == nil {
    r.LoadAutoContext(ctx)
  }

  onInterrupt := r.Interrupt
  interrupt.Store(&onInterrupt)

  if err := r.Run(ctx); err != nil {
    return fmt.Errorf("\n\t... in run:\n\t%w", err)
  }

  if !flags.noSave {
    if err := r.SaveNow(); err != nil {
      ui.Error("failed to save session on exit: %s", err)
    } else if r.Meta.Title != "" || len(r.History) > 1 {
      fmt.Println(ui.Dim("resume with: armin -resume " + repl.ShortID(r.Meta.ID)))
    }
  }

  return nil
}

// remoteSpec builds the cloud-machine part of a model, if it has
// one, filling defaults: the Ubuntu image user, llama-server's
// default port, and the usual region variables.
func remoteSpec(c config.ModelSpecConfig) *llamacpp.RemoteSpec {
  r := &llamacpp.RemoteSpec{
    SSHUser:          firstNonEmpty(c.SSHUser, "ubuntu"),
    SSHKey:           c.SSHKey,
    RemotePort:       firstNonZeroInt(c.RemotePort, 8080),
    StopOnDisconnect: !c.KeepOnDisconnect,
  }

  switch {
  case c.AWSInstance != "":
    r.Provider, r.MachineID = llamacpp.ProviderAWS, c.AWSInstance
    r.Region = firstNonEmpty(
      c.AWSRegion,
      firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION")),
    )
  case c.StackitServer != "":
    r.Provider, r.MachineID = llamacpp.ProviderSTACKIT, c.StackitServer
    r.ProjectID = firstNonEmpty(c.StackitProject, os.Getenv("STACKIT_PROJECT_ID"))
    r.Region = firstNonEmpty(
      c.StackitRegion, firstNonEmpty(os.Getenv("STACKIT_REGION"), "eu01"),
    )
  case c.ScalewayServer != "":
    r.Provider, r.MachineID = llamacpp.ProviderScaleway, c.ScalewayServer
    r.Region = firstNonEmpty(
      c.ScalewayZone, firstNonEmpty(os.Getenv("SCW_DEFAULT_ZONE"), "fr-par-2"),
    )
    // Scaleway's GPU images log in as root.
    r.SSHUser = firstNonEmpty(c.SSHUser, "root")
  default:
    return nil
  }

  return r
}

// pickSession shows the saved sessions and reads a choice: a menu
// number, an id (or prefix), or Enter for a new session.
func pickSession(root string) (*session.Saved, error) {
  list, err := session.List()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in pickSession:\n\t%w", err)
  }

  if len(list) == 0 {
    ui.Info("no saved sessions yet — starting a new one")
    return nil, nil
  }

  abs, _ := filepath.Abs(root)
  fmt.Println(ui.Bold("resume a session") + ui.Dim("  (* = this directory)"))
  repl.PrintSessions(list, abs, "", true)
  fmt.Print("\nnumber or id, Enter for a new session: ")

  line := strings.TrimSpace(readLine(os.Stdin))
  if line == "" {
    return nil, nil
  }

  if n, err := strconv.Atoi(line); err == nil {
    if n < 1 || n > min(len(list), 25) {
      return nil, fmt.Errorf("\n\t... in pickSession:\n\t no session %d in the list", n)
    }

    return session.Load(list[n - 1].ID)
  }

  return session.Load(line)
}

// splitList splits a comma-separated flag value, dropping empties.
func splitList(s string) []string {
  var out []string
  for _, part := range strings.Split(s, ",") {
    if part = strings.TrimSpace(part); part != "" {
      out = append(out, part)
    }
  }

  return out
}

// readLine reads up to a newline one byte at a time: a buffered
// reader would swallow input meant for the REPL that starts next.
func readLine(r io.Reader) string {
  var b strings.Builder
  var c [1]byte
  for {
    n, err := r.Read(c[:])
    if n == 1 {
      if c[0] == '\n' {
        break
      }

      b.WriteByte(c[0])
    }

    if err != nil {
      break
    }
  }

  return b.String()
}

// queryContextSize asks the server for its window, briefly — a
// remote endpoint without /props just leaves auto-compaction off.
func queryContextSize(ctx context.Context, client *llm.Client) int {
  ctx, cancel := context.WithTimeout(ctx, 5 * time.Second)
  defer cancel()
  n, err := client.ContextSize(ctx)
  if err != nil {
    return 0
  }

  return n
}

// openNotes sets up memories (per project and per user), skills
// (~/.config/armin/skills and <root>/.armin/skills) and the
// project's scratchpad.
func openNotes(root string) (
  *notes.Memories, *notes.Skills, *notes.Scratchpad, error,
) {
  projectMem, err := session.ProjectDataDir("memory", root)
  if err != nil {
    return nil, nil, nil, fmt.Errorf("\n\t... in openNotes:\n\t%w", err)
  }

  data, err := session.DataDir()
  if err != nil {
    return nil, nil, nil, fmt.Errorf("\n\t... in openNotes:\n\t%w", err)
  }

  scratchDir, err := session.ProjectDataDir("scratch", root)
  if err != nil {
    return nil, nil, nil, fmt.Errorf("\n\t... in openNotes:\n\t%w", err)
  }

  confDir, err := config.Dir()
  if err != nil {
    return nil, nil, nil, fmt.Errorf("\n\t... in openNotes:\n\t%w", err)
  }

  mem := &notes.Memories{
    ProjectDir: projectMem,
    UserDir:    filepath.Join(data, "memory", "user"),
  }

  skills := notes.LoadSkills(
    filepath.Join(confDir, "skills"),
    filepath.Join(root, ".armin", "skills"),
  )

  return mem, skills, &notes.Scratchpad{ Dir: scratchDir }, nil
}

// buildModelSpecs merges config.conf's models.<name> blocks with
// the zero-config fallback: a name with no matching Models entry
// (the common case for someone who just started a bare llama-server
// by hand) becomes an external, unmanaged spec at -llama-url /
// -completion-llama-url — so "point armin at a server you already
// run" keeps working without writing a config file at all, and
// writing models.<name>.binary/model_path entries is only needed
// once you want armin to spawn and reap the process itself.
func buildModelSpecs(
  fileCfg config.Config, flags cliFlags,
) map[string]llamacpp.ModelSpec {
  specs := make(map[string]llamacpp.ModelSpec, len(fileCfg.Models) + 2)
  for name, c := range fileCfg.Models {
    spec := llamacpp.ModelSpec{
      URL: c.URL, APIKeyEnv: c.APIKeyEnv,
      Binary: c.Binary, ModelPath: c.ModelPath,
      Port: c.Port, Args: c.Args, IdleTimeout: c.IdleTimeout,
    }

    if c.Vision {
      spec.Flags |= llamacpp.SpecVision
    }

    if c.NoLean {
      spec.Flags |= llamacpp.SpecNoLean
    }

    if c.StreamContext > 0 {
      spec.Stream = &llamacpp.StreamSpec{
        Binary:   c.StreamBinary,
        Context:  c.StreamContext,
        ArenaMiB: c.StreamArenaMiB,
        Args:     c.StreamArgs,
      }
    }

    if r := remoteSpec(c); r != nil {
      spec.Remote = r
      spec.Binary = firstNonEmpty(spec.Binary, "llama-server")
      // 18180, not 18080: that one is the local SearXNG container's.
      spec.Port = firstNonZeroInt(spec.Port, 18180)
    }

    specs[name] = spec
  }

  if _, ok := specs[flags.model]; !ok {
    specs[flags.model] = llamacpp.ModelSpec{ URL: flags.llamaURL }
  }

  if _, ok := specs[flags.completionModel]; !ok {
    url := flags.completionLlamaURL
    if url == "" {
      url = flags.llamaURL
    }

    specs[flags.completionModel] = llamacpp.ModelSpec{ URL: url }
  }

  return specs
}

// cliFlags holds every parsed flag — one struct instead of a
// scattering of *string/*bool locals, so run() reads its config
// from one place.
type cliFlags struct {
  root, model, completionModel, ctagsBin string
  webAddr, addDirs                       string
  llamaURL, completionLlamaURL           string
  timeout                                time.Duration
  maxOutput, maxRounds                   int
  resume                                 string // "": no; "menu"; else a session id
  dryRun, mcpMode, lspMode, noSave, noColor bool
  autoApprove                                       bool
}

func parseFlags(fileCfg config.Config) cliFlags {
  var f cliFlags

  defaultModel := firstNonEmpty(fileCfg.Model, "chat")

  flag.StringVar(&f.root, "root", firstNonEmpty(fileCfg.Root, "."),
    "project root to operate in (paths are jailed to this directory)")
  flag.StringVar(&f.model, "model", defaultModel,
    "name of the model to use for chat — either a models.<name> "+
      "entry from config.conf, or (with no matching entry) an "+
      "ad-hoc label for whatever's running at -llama-url")
  flag.StringVar(&f.completionModel, "completion-model",
    firstNonEmpty(fileCfg.CompletionModel, defaultModel),
    "name of the model for -lsp inline completion; should be a "+
      "fill-in-the-middle code model, not a chat model. Defaults to "+
      "-model itself, i.e. reusing one llama-server for both")
  flag.StringVar(&f.ctagsBin, "ctags-bin",
    firstNonEmpty(fileCfg.CtagsBinary, resolveCtagsBinary()),
    "Universal Ctags binary for -lsp definition/workspace-symbol/"+
      "hover grounding (empty disables those, degrading gracefully)")
  flag.StringVar(&f.llamaURL, "llama-url",
    firstNonEmpty(fileCfg.LlamaURL, "http://localhost:8080"),
    "llama-server base URL used for -model when it has no "+
      "matching models.<name> entry in config.conf")
  flag.StringVar(&f.completionLlamaURL, "completion-llama-url",
    fileCfg.CompletionLlamaURL,
    "llama-server base URL used for -completion-model when it has "+
      "no matching models.<name> entry; empty reuses -llama-url")
  flag.DurationVar(&f.timeout, "timeout",
    firstNonZeroDuration(fileCfg.Timeout, 20 * time.Second),
    "per-subprocess timeout")
  flag.IntVar(&f.maxOutput, "max-output",
    firstNonZeroInt(fileCfg.MaxOutput, 4000),
    "max bytes of tool output before truncation")
  flag.IntVar(&f.maxRounds, "max-rounds",
    firstNonZeroInt(fileCfg.MaxToolRounds, agent.DefaultMaxRounds),
    "max tool-call rounds per turn before the model must report back")
  flag.StringVar(&f.addDirs, "add-dir", "",
    "comma-separated directories outside the project armin may read "+
      "(never write), e.g. -add-dir .. for slides next to the repo")
  flag.StringVar(&f.webAddr, "web", "",
    "serve the chat in a browser at this localhost address "+
      "(e.g. 127.0.0.1:7777) instead of the REPL")
  flag.BoolVar(&f.autoApprove, "yes", false,
    "apply file writes without asking first")
  flag.BoolVar(&f.dryRun, "dry-run", false,
    "describe what run_command would execute instead of running it")
  flag.BoolVar(&f.mcpMode, "mcp", false,
    "serve the sandboxed tools over MCP stdio instead of the REPL")
  flag.BoolVar(&f.lspMode, "lsp", false,
    "serve hover + an ask command over LSP stdio instead of the REPL")
  flag.Var((*resumeFlag)(&f.resume), "resume",
    "resume a saved session: alone, pick from a menu of sessions; "+
      "-resume <id> (or a unique prefix of it) resumes that one")
  flag.BoolVar(&f.noSave, "no-save", false,
    "don't persist the session to disk after each turn")
  flag.BoolVar(&f.noColor, "no-color", false, "disable ANSI styling")

  flag.Usage = func() {
    fmt.Fprintln(os.Stderr, "Usage: armin [path] [flags]")
    fmt.Fprintln(os.Stderr)
    fmt.Fprintln(os.Stderr, "path is shorthand for -root (\"armin .\" == \"armin -root .\"); giving both is an error.")
    fmt.Fprintln(os.Stderr)
    flag.PrintDefaults()
  }

  // Go's flag package stops parsing flags at the first non-flag
  // argument, so "armin . -lsp" would otherwise silently treat
  // "-lsp" as a second positional argument instead of a flag.
  // Pulling the path out first, wherever it sits in argv, lets it go
  // anywhere on the command line, not just at the very end.
  rest, positional := extractPositionalRoot(os.Args[1:])
  flag.CommandLine.Parse(rest)

  if positional != "" {
    if rootExplicitlySet() {
      fmt.Fprintln(os.Stderr,
        "armin: both -root and a positional path were given — use one or the other")
      os.Exit(2)
    }

    f.root = positional
  }

  if flag.NArg() > 0 {
    fmt.Fprintf(os.Stderr, "armin: too many positional arguments: %v\n", flag.Args())
    os.Exit(2)
  }

  return f
}

// resumeFlag is -resume: a bool flag that can also carry a value.
// "-resume" alone means "show the menu"; "-resume=<id>" (or
// "-resume <id>", see extractPositionalRoot) names a session.
type resumeFlag string

func (f *resumeFlag) String() string   { return string(*f) }
func (f *resumeFlag) IsBoolFlag() bool { return true }

func (f *resumeFlag) Set(v string) error {
  switch v {
  case "true":
    *f = "menu"
  case "false":
    *f = ""
  default:
    *f = resumeFlag(v)
  }

  return nil
}

// boolFlagNames lists every flag registered above that takes no
// value (bool flags): extractPositionalRoot needs to know which
// "-name" tokens consume the following argv element and which
// don't, the same distinction flag.Parse itself makes internally.
var boolFlagNames = map[string]bool{
  "dry-run": true, "mcp": true, "lsp": true, "yes": true,
  "resume": true, "no-save": true, "no-color": true,
  "h": true, "help": true,
}

// extractPositionalRoot pulls the first bare (non-flag) token out of
// args — wherever it appears — and returns the remaining tokens
// (still in order, flags intact) for flag.Parse, plus that token (or
// "" if there wasn't one). A "-flag value" pair, non-bool flags
// included, is kept together and skipped over rather than
// mistaken for two positional arguments.
func extractPositionalRoot(args []string) (rest []string, positional string) {
  rest = make([]string, 0, len(args))

  for i := 0; i < len(args); i++ {
    a := args[i]

    switch {
    case a == "--":
      rest = append(rest, args[i:]...)

      return rest, positional

    case !strings.HasPrefix(a, "-") || a == "-":
      if positional == "" {
        positional = a
      } else {
        rest = append(rest, a) // a second bare token — flag.NArg() > 0 reports this as an error
      }

    case strings.Contains(a, "="):
      rest = append(rest, a) // "-flag=value": self-contained, nothing to skip

    default:
      rest = append(rest, a)
      name := strings.TrimLeft(a, "-")

      // "-resume <id>": the next word is a session id unless it's
      // a directory (then it's the project root, as before).
      if name == "resume" && i + 1 < len(args) && looksLikeSessionID(args[i + 1]) {
        i++
        rest[len(rest) - 1] = "-resume=" + args[i]
        continue
      }

      if !boolFlagNames[name] && i + 1 < len(args) {
        i++
        rest = append(rest, args[i])
      }
    }
  }

  return rest, positional
}

// looksLikeSessionID: not a flag, not an existing directory.
func looksLikeSessionID(s string) bool {
  if s == "" || strings.HasPrefix(s, "-") {
    return false
  }

  info, err := os.Stat(s)

  return err != nil || !info.IsDir()
}

// rootExplicitlySet reports whether -root was actually passed on the
// command line, as opposed to just holding its (fileCfg- or
// hardcoded-) default — flag.Visit only calls back for flags an
// argument actually set.
func rootExplicitlySet() bool {
  set := false
  flag.Visit(func(fl *flag.Flag) {
    if fl.Name == "root" {
      set = true
    }
  })

  return set
}

// resolveCtagsBinary looks for a Universal Ctags binary without
// spawning it: PATH first (as "universal-ctags", the name that
// avoids colliding with Emacs's incompatible /usr/bin/ctags on many
// distros), then the user-local install location armin's own docs
// recommend building it to. Empty means the -lsp symbol features
// degrade gracefully — see internal/ctags.
func resolveCtagsBinary() string {
  if p, err := exec.LookPath("universal-ctags"); err == nil {
    return p
  }

  if home, err := os.UserHomeDir(); err == nil {
    p := filepath.Join(home, ".local", "bin", "universal-ctags")
    if _, err := os.Stat(p); err == nil {
      return p
    }
  }

  return ""
}

func firstNonEmpty(a, b string) string {
  if a != "" {
    return a
  }

  return b
}

func firstNonZeroDuration(a, b time.Duration) time.Duration {
  if a != 0 {
    return a
  }

  return b
}

func firstNonZeroInt(a, b int) int {
  if a != 0 {
    return a
  }

  return b
}
