// Command-line flags. parseFlags declares every flag with its default
// from config.conf (flags win), and pulls a bare project path ("armin
// .") out of argv wherever it sits, since Go's flag package stops at
// the first non-flag. Parse (input.go) turns the result into Options.

package input

import (
  "flag"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/config"
)

// splitList splits a comma-separated flag value, dropping empties.
func splitList(s string) []string {
  var out []string
  for _, part := range strings.Split(s, ",") {
    part = strings.TrimSpace(part)
    if part != "" {
      out = append(out, part)
    }
  }

  return out
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
  flag.BoolVar(&f.newInstance, "new", false,
    "start a separate armin even when nvim's armin serves this "+
      "project (by default the TUI joins its :ArminAsk chat)")
  flag.Var((*resumeFlag)(&f.resume), "resume",
    "resume a saved session: alone, pick from a menu of sessions; "+
      "-resume <id> (or a unique prefix of it) resumes that one")
  flag.BoolVar(&f.noSave, "no-save", false,
    "don't persist the session to disk after each turn")
  flag.BoolVar(&f.noColor, "no-color", false, "disable ANSI styling")
  flag.StringVar(&f.effort, "effort", firstNonEmpty(fileCfg.Effort, "auto"),
    "how hard the model thinks: auto (it judges each request), off, "+
      "low, medium or high; /effort changes it in the chat")
  flag.StringVar(&f.aids, "aids", firstNonEmpty(fileCfg.Aids, "all"),
    "reasoning aids: all, none, or a list of stuck (step back after "+
      "repeated failures), procedure (steps per kind of task), verify "+
      "(check the work before answering)")
  flag.BoolVar(&f.librarySync, "library-sync", false,
    "index the reference library (library = in config.conf) and have "+
      "-model catalog the books the topic table lacks, then exit")
  flag.StringVar(&f.libraryAdd, "library-add", "",
    "comma-separated files or folders to copy into the library, "+
      "then index and catalog them, and exit")
  flag.StringVar(&f.libraryTo, "library-to", "added",
    "the library folder (topic) -library-add copies into, e.g. go, math")
  flag.StringVar(&f.install, "install", "",
    "size up a model against this machine, then download it and add "+
      "it to config.conf: org/repo[:QUANT] on Hugging Face, "+
      "org/repo/file.gguf, or a .gguf already on disk")
  flag.StringVar(&f.installTo, "install-to", "",
    "the folder -install downloads into (default: where your models "+
      "already are, else ~/models)")

  flag.Usage = func() {
    fmt.Fprintln(os.Stderr, "Usage: armin [path] [flags]")
    fmt.Fprintln(os.Stderr)
    fmt.Fprintln(os.Stderr, "path is shorthand for -root "+
      "(\"armin .\" == \"armin -root .\"); giving both is an error.")
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
    info, err := os.Stat(positional)
    if err != nil || !info.IsDir() {
      if positional == "help" {
        flag.Usage()
        os.Exit(0)
      }

      fmt.Fprintf(os.Stderr,
        "armin: %q isn't a directory. armin takes a project folder, not a "+
          "command: armin [path] [flags] (armin -h lists the flags)\n",
        positional)
      os.Exit(2)
    }

    if rootExplicitlySet() {
      fmt.Fprintln(os.Stderr, "armin: both -root and a positional "+
        "path were given — use one or the other")
      os.Exit(2)
    }

    f.root = positional
  }

  if flag.NArg() > 0 {
    fmt.Fprintf(os.Stderr,
      "armin: too many positional arguments: %v\n", flag.Args())
    os.Exit(2)
  }

  return f
}

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
  "dry-run":  true,
  "mcp":      true,
  "lsp":      true,
  "new":      true,
  "yes":      true,
  "resume":   true,
  "no-save":  true,
  "no-color": true,
  "library-sync": true,
  "h":        true,
  "help":     true,
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
        // a second bare token: flag.NArg() > 0 reports the error
        rest = append(rest, a)
      }

    case strings.Contains(a, "="):
      rest = append(rest, a) // "-flag=value": self-contained, nothing to skip

    default:
      rest = append(rest, a)
      name := strings.TrimLeft(a, "-")

      // "-resume <id>": the next word is a session id unless it's
      // a directory (then it's the project root, as before).
      next := i + 1 < len(args)
      if name == "resume" && next && looksLikeSessionID(args[i + 1]) {
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

// resolveCtagsBinary looks for a Universal Ctags binary without
// spawning it: PATH first (as "universal-ctags", the name that
// avoids colliding with Emacs's incompatible /usr/bin/ctags on many
// distros), then the user-local install location armin's own docs
// recommend building it to. Empty means the -lsp symbol features
// degrade gracefully — see internal/ctags.
func resolveCtagsBinary() string {
  p, err := exec.LookPath("universal-ctags")
  if err == nil {
    return p
  }

  home, err := os.UserHomeDir()
  if err == nil {
    p := filepath.Join(home, ".local", "bin", "universal-ctags")
    _, err := os.Stat(p)
    if err == nil {
      return p
    }
  }

  return ""
}
