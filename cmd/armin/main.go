// Command armin is a terminal chat assistant, backed by a local
// Ollama model, that can search/read a codebase and run whitelisted
// shell commands to inspect it. Run with -mcp to instead expose the
// same sandboxed tools as an MCP stdio server, or -lsp to expose
// hover + a single ask command over the Language Server Protocol
// (for editor integration — see docs/nvim.md).
package main

import (
  "context"
  "flag"
  "fmt"
  "os"
  "os/exec"
  "os/signal"
  "path/filepath"
  "time"

  "armin/internal/agent"
  "armin/internal/config"
  "armin/internal/llm"
  "armin/internal/lspserver"
  "armin/internal/mcpserver"
  "armin/internal/repl"
  "armin/internal/sandbox"
  "armin/internal/session"
  "armin/internal/ui"
)

const systemPrompt = `You are a terminal assistant that helps a developer understand and work in their codebase.
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

  if flags.mcpMode {
    return mcpserver.Serve(ctx, sb)
  }

  client := llm.New(flags.ollamaURL, flags.model)
  loop := agent.New(client, sb)

  if flags.lspMode {
    server := lspserver.New(
      loop, sb, systemPrompt, flags.completionModel, flags.ctagsBin,
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
      client.Model = resumed.Model
    }
  }

  r := repl.New(
    loop, client, sb, systemPrompt, resumed, !flags.noSave,
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

// cliFlags holds every parsed flag — one struct instead of a
// scattering of *string/*bool locals, so run() reads its config
// from one place.
type cliFlags struct {
  root, model, completionModel, ctagsBin, ollamaURL string
  timeout                                           time.Duration
  maxOutput                                         int
  dryRun, mcpMode, lspMode, resume, noSave, noColor bool
}

func parseFlags(fileCfg config.Config) cliFlags {
  var f cliFlags

  flag.StringVar(&f.root, "root", firstNonEmpty(fileCfg.Root, "."),
    "project root to operate in (paths are jailed to this directory)")
  flag.StringVar(&f.model, "model", firstNonEmpty(fileCfg.Model, "qwen3:4b"),
    "Ollama model name")
  flag.StringVar(&f.completionModel, "completion-model",
    firstNonEmpty(fileCfg.CompletionModel, "qwen2.5-coder:1.5b-base"),
    "Ollama model for -lsp inline completion; should be a "+
      "fill-in-the-middle code model, not a chat model")
  flag.StringVar(&f.ctagsBin, "ctags-bin",
    firstNonEmpty(fileCfg.CtagsBinary, resolveCtagsBinary()),
    "Universal Ctags binary for -lsp definition/workspace-symbol/"+
      "hover grounding (empty disables those, degrading gracefully)")
  flag.StringVar(&f.ollamaURL, "ollama-url",
    firstNonEmpty(fileCfg.OllamaURL, "http://localhost:11434"),
    "Ollama base URL")
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

  flag.Parse()
  return f
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
