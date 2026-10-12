// Package input is everything armin is told before it starts: the
// command line, config.conf and .env, which session to resume, and
// the signals that arrive while it runs.
package input

import (
  "fmt"
  "path/filepath"

  "armin/internal/config"
  "armin/internal/session"
  "armin/internal/ui"
)

const (
  ModeREPL Mode = iota // the terminal chat
  ModeWeb              // -web: the chat in a browser
  ModeLSP              // -lsp: nvim's hover, completion and :ArminAsk
  ModeMCP              // -mcp: the tools alone, for another agent
)

const (
  FlagDryRun      Flag = 0b00000001 // describe commands instead of running them
  FlagNoSave      Flag = 0b00000010 // don't persist the session
  FlagNoColor     Flag = 0b00000100 // no ANSI styling
  FlagAutoApprove Flag = 0b00001000 // apply writes without asking
  FlagNew         Flag = 0b00010000 // never join nvim's armin
  FlagAll         Flag = 0b00011111
)

func (o *Options) Has(f Flag) bool { return o.Flags & f != 0 }

// Parse reads .env, config.conf and the command line, and asks which
// session to resume when -resume came alone.
func Parse() (*Options, error) {
  // Before config.Load(), so a models.<name>.api_key_env entry can
  // resolve against a key .env only just populated.
  err := config.LoadEnv()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Parse:\n\t loading .env: %w", err)
  }

  fileCfg, err := config.Load()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Parse:\n\t loading config: %w", err)
  }

  f := parseFlags(fileCfg)

  // Picked before the sandbox exists: a session from another
  // directory resumes there.
  var resumed *session.Saved
  switch f.resume {
  case "":
  case "menu":
    resumed, err = pickSession(f.root)
  default:
    resumed, err = session.Load(f.resume)
  }

  if err != nil {
    return nil, fmt.Errorf("\n\t... in Parse:\n\t resuming: %w", err)
  }

  if resumed != nil && resumed.Root != "" {
    f.root = resumed.Root
  }

  if f.noColor {
    ui.Enabled = false
  }

  // The sandbox's own resolution, so a socket keyed on the root
  // (see display.Join) matches between nvim's armin and this one.
  root, err := filepath.Abs(f.root)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Parse:\n\t%w", err)
  }

  resolved, err := filepath.EvalSymlinks(root)
  if err == nil {
    root = resolved
  }

  o := &Options{
    Root:               root,
    Model:              f.model,
    CompletionModel:    f.completionModel,
    LlamaURL:           f.llamaURL,
    CompletionLlamaURL: f.completionLlamaURL,
    CtagsBin:           f.ctagsBin,
    WebAddr:            f.webAddr,
    AddDirs:            splitList(f.addDirs),
    Timeout:            f.timeout,
    MaxOutput:          f.maxOutput,
    MaxRounds:          f.maxRounds,
    Config:             fileCfg,
    Resumed:            resumed,
  }

  switch {
  case f.mcpMode:
    o.Mode = ModeMCP
  case f.lspMode:
    o.Mode = ModeLSP
  case f.webAddr != "":
    o.Mode = ModeWeb
  }

  set := func(on bool, flag Flag) {
    if on {
      o.Flags |= flag
    }
  }

  set(f.dryRun, FlagDryRun)
  set(f.noSave, FlagNoSave)
  set(f.noColor, FlagNoColor)
  set(f.autoApprove || fileCfg.AutoApprove, FlagAutoApprove)
  set(f.newInstance, FlagNew)

  return o, nil
}
