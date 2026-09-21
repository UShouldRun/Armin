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
  "flag"
  "fmt"
  "os"
  "os/exec"
  "os/signal"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/config"
  "armin/internal/ctags"
  "armin/internal/llamacpp"
  "armin/internal/lspserver"
  "armin/internal/mcpserver"
  "armin/internal/repl"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/session"
  "armin/internal/ui"
)

const systemPrompt = `You are a terminal assistant named Armin that helps a developer understand and work in their codebase.
You have tools to search, read, and list files in the project, plus a narrow set of read-only shell commands.
Ground every answer in what the tools actually return — don't guess at file contents or project structure.
Prefer search_code before read_file on anything you haven't already located.
Keep answers concise; this is a terminal, not a document.`

func main() {
  if err := run(); err != nil {
    ui.Error("%s", err)
    os.Exit(1)
  }
}

func run() error {
  fileCfg, err := config.Load()
  if err != nil {
    return fmt.Errorf("\n\t... in run:\n\t loading config: %w", err)
  }

  flags := parseFlags(fileCfg)

  if flags.noColor {
    ui.Enabled = false
  }

  allowlist := sandbox.DefaultAllowlist()
  for _, bin := range fileCfg.ExtraAllowed {
    allowlist[bin] = true
  }

  sb, err := sandbox.New(flags.root, sandbox.Options{
    Timeout:   flags.timeout,
    MaxOutput: flags.maxOutput,
    Allowlist: allowlist,
    DryRun:    flags.dryRun,
  })
  if err != nil {
    return fmt.Errorf(
      "\n\t... in run:\n\t setting up sandbox: %w", err,
    )
  }

  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  stop := make(chan os.Signal, 1)
  signal.Notify(stop, os.Interrupt)
  go func() {
    <-stop
    cancel()
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
        context.Background(), 15*time.Second,
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
  defer func() {
    shutdownCtx, cancel := context.WithTimeout(
      context.Background(), 15*time.Second,
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

  if flags.lspMode {
    completionClient, err := models.Client(ctx, flags.completionModel)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in run:\n\t starting completion model %q: %w",
        flags.completionModel, err,
      )
    }
    server := lspserver.New(
      loop, sb, systemPrompt, completionClient, models, symbols,
    )
    if err := server.Serve(ctx, os.Stdin, os.Stdout); err != nil {
      return fmt.Errorf("\n\t... in run:\n\t%w", err)
    }
    return nil
  }

  var resumed *session.Saved
  if flags.resume {
    resumed, err = session.Load(sb.Root)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in run:\n\t resuming session: %w", err,
      )
    }
    if resumed != nil {
      if err := models.Switch(ctx, client, resumed.Model); err != nil {
        ui.Error(
          "resuming with model %q: %s (staying on %q)",
          resumed.Model, err, flags.model,
        )
      }
    }
  }

  r := repl.New(
    loop, client, sb, systemPrompt, resumed, !flags.noSave, models,
  )
  if resumed == nil {
    r.LoadAutoContext(ctx)
  }

  if err := r.Run(ctx); err != nil {
    return fmt.Errorf("\n\t... in run:\n\t%w", err)
  }

  if !flags.noSave {
    err := session.Save(sb.Root, client.Model, r.History)
    if err != nil {
      ui.Error("failed to save session on exit: %s", err)
    }
  }

  return nil
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
  specs := make(map[string]llamacpp.ModelSpec, len(fileCfg.Models)+2)
  for name, c := range fileCfg.Models {
    specs[name] = llamacpp.ModelSpec{
      URL: c.URL, Binary: c.Binary, ModelPath: c.ModelPath,
      Port: c.Port, Args: c.Args, IdleTimeout: c.IdleTimeout,
    }
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
  llamaURL, completionLlamaURL           string
  timeout                                time.Duration
  maxOutput                              int
  dryRun, mcpMode, lspMode, resume, noSave, noColor bool
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
    firstNonZeroDuration(fileCfg.Timeout, 20*time.Second),
    "per-subprocess timeout")
  flag.IntVar(&f.maxOutput, "max-output",
    firstNonZeroInt(fileCfg.MaxOutput, 4000),
    "max bytes of tool output before truncation")
  flag.BoolVar(&f.dryRun, "dry-run", false,
    "describe what run_command would execute instead of running it")
  flag.BoolVar(&f.mcpMode, "mcp", false,
    "serve the sandboxed tools over MCP stdio instead of the REPL")
  flag.BoolVar(&f.lspMode, "lsp", false,
    "serve hover + an ask command over LSP stdio instead of the REPL")
  flag.BoolVar(&f.resume, "resume", false,
    "resume the most recent saved session for this project root")
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

// boolFlagNames lists every flag registered above that takes no
// value (bool flags): extractPositionalRoot needs to know which
// "-name" tokens consume the following argv element and which
// don't, the same distinction flag.Parse itself makes internally.
var boolFlagNames = map[string]bool{
  "dry-run": true, "mcp": true, "lsp": true,
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
      if !boolFlagNames[name] && i+1 < len(args) {
        i++
        rest = append(rest, args[i])
      }
    }
  }
  return rest, positional
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
