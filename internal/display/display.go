// Package display is how armin is shown: the terminal chat, the
// browser chat, nvim over LSP, the tools over MCP, or a terminal
// joining the project's running instance. It's main's last step
// (input → think → display): Run picks the frontend for the mode and
// hands it the Mind.
//
//   display.go  Join (become part of a running instance instead of
//               loading models), Run, and the REPL and web setups
//   lsp.go      -lsp: nvim's frontend, alone or joined to an instance
//   choose.go   the OpenRouter model picker shown before connecting
//   library.go  -library-sync and -library-add
//   install.go  -install, which needs no Mind
//
// The frontends themselves live in their own packages: repl, web,
// lspserver, mcpserver; share connects instances in one project.
package display

import (
  "context"
  "fmt"
  "os"
  "sync/atomic"

  "armin/internal/agent"
  "armin/internal/build"
  "armin/internal/input"
  "armin/internal/mcpserver"
  "armin/internal/repl"
  "armin/internal/share"
  "armin/internal/think"
  "armin/internal/ui"
  "armin/internal/web"
)

// Join makes this armin part of the project's instance when one
// runs — started first, by a terminal or by nvim — instead of loading
// models of its own: a terminal shares its chat, nvim's -lsp sends
// it chat, hover and completion. False means start normally (and
// become the instance).
func Join(
  ctx context.Context, opts *input.Options, sig *input.Signals,
) (bool, error) {
  if opts.Resumed != nil || opts.Has(input.FlagNew) {
    return false, nil
  }

  switch opts.Mode {
  case input.ModeREPL:
    attached, err := repl.DialAttached(opts.Root)
    if err != nil {
      return false, nil
    }

    sig.OnInterrupt(attached.Interrupt)

    return true, attached.Run(ctx)

  case input.ModeLSP:
    host, err := share.Dial(opts.Root, "nvim")
    if err != nil {
      return false, nil
    }

    defer host.Close()

    return true, joinLSP(ctx, opts, host)
  }

  return false, nil
}

// Run shows mind in the frontend opts asked for, until it ends.
func Run(
  ctx context.Context, opts *input.Options, mind *think.Mind,
  sig *input.Signals,
) error {
  if os.Getenv("ARMIN_STARTUP") != "" {
    mind.PrintStartup(os.Stderr)
  }

  // background work's messages: the REPL takes them over (runREPL
  // starts its drain once it exists, so startup notices land under
  // the banner, not on stderr above it); the other modes print
  // them to stderr
  switch opts.Mode {
  case input.ModeMCP:
    go printNotices(mind)
    return mcpserver.Serve(ctx, mind.Sandbox, mind.WebSearch, mind.Symbols)
  case input.ModeLSP:
    go printNotices(mind)
    return runLSP(ctx, opts, mind)
  case input.ModeWeb:
    go printNotices(mind)
    return runWeb(ctx, opts, mind)
  case input.ModeLibrary:
    go printNotices(mind)
    return runLibrary(ctx, opts, mind)
  }

  return runREPL(ctx, opts, mind, sig)
}

func runWeb(ctx context.Context, opts *input.Options, mind *think.Mind) error {
  for _, dir := range opts.AddDirs {
    _, err := mind.Sandbox.AddReadRoot(dir)
    if err != nil {
      ui.Error("-add-dir %s: %s", dir, err)
    }
  }

  return web.Serve(ctx, web.Config{
    Addr:         opts.WebAddr,
    Loop:         mind.Loop,
    Client:       mind.Client,
    Models:       mind.Models,
    Sandbox:      mind.Sandbox,
    SystemPrompt: mind.SystemPrompt(false),
    AutoApprove:  opts.Has(input.FlagAutoApprove),
    Resumed:      opts.Resumed,
    Autosave:     !opts.Has(input.FlagNoSave),
    ScratchBase:  mind.Scratch.Dir,
    Effort:       opts.Effort,
  })
}

func runREPL(
  ctx context.Context, opts *input.Options, mind *think.Mind,
  sig *input.Signals,
) error {
  save := !opts.Has(input.FlagNoSave)
  r := repl.New(
    mind.Loop, mind.Client, mind.Sandbox, mind.SystemPrompt(false),
    opts.Resumed, save, mind.Models,
  )

  r.Completion, r.Follower = mind.Completion, mind.Follower()
  // stored before the drain starts, and before Run prints the
  // banner: startup notices (the knowledge DAG summary) then
  // surface at the first prompt, under the header
  notices.Store(r)
  go printNotices(mind)
  mind.Loop.Session = r.Meta.ID // who writes to the knowledge DAG
  r.STT, r.STTRecord = opts.Config.STT, opts.Config.STTRecord
  r.RoleModels = map[agent.Role]string{
    agent.RoleMinion:  opts.Config.Minion,
    agent.RoleCopilot: opts.Config.Copilot,
    agent.RoleAdvisor: opts.Config.Advisor,
  }

  // starting on a role's model starts in that role
  for role, model := range r.RoleModels {
    if model != "" && model == opts.Model {
      mind.Loop.Role = role
    }
  }

  completing := mind.Completion.Model
  r.SetCompleting(completing)

  // The first armin in a project is its instance (see Join).
  hub, err := share.Listen(ctx, mind.Sandbox.Root, r.Host())
  if err == nil {
    r.Share(hub)
    defer hub.Close()
  }

  r.SetAutoApprove(opts.Has(input.FlagAutoApprove))
  r.SetEffort(opts.Effort)
  r.Terminal = opts.Config.Terminal
  r.ScratchBase = mind.Scratch.Dir
  r.BindScratch()
  for _, dir := range opts.AddDirs {
    err := r.AddDir(dir)
    if err != nil {
      ui.Error("-add-dir %s: %s", dir, err)
    }
  }

  r.ContextFromServer = opts.Config.ContextSize == 0
  if opts.Resumed == nil {
    r.LoadAutoContext(ctx)
  }

  sig.OnInterrupt(r.Interrupt)

  err = r.Run(ctx)
  if err != nil {
    return fmt.Errorf("\n\t... in runREPL:\n\t%w", err)
  }

  if !save {
    return nil
  }

  err = r.SaveNow()
  if err != nil {
    ui.Error("failed to save session on exit: %s", err)
  } else if r.Meta.Title != "" || len(r.History) > 1 {
    id := repl.ShortID(r.Meta.ID)
    fmt.Println(ui.Dim("resume with: " + build.Name + " -resume " + id))
  }

  return nil
}

// notices is the REPL that shows background notices, once there is
// one; nil until then.
var notices atomic.Pointer[repl.REPL]

// printNotices shows each background notice: at the REPL's prompt when
// it runs, else on stderr.
func printNotices(mind *think.Mind) {
  for msg := range mind.Notices {
    r := notices.Load()
    if r != nil {
      r.Notice(msg)
      continue
    }

    fmt.Fprintln(os.Stderr, ui.Dim("  " + msg))
  }
}

