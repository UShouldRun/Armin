// Package display is how armin is shown: the terminal chat, the
// browser chat, nvim over LSP, the tools over MCP — or, when nvim's
// armin already serves the project, a terminal sharing its chat.
package display

import (
  "context"
  "fmt"

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
func Join(ctx context.Context, opts *input.Options, sig *input.Signals) (bool, error) {
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
  switch opts.Mode {
  case input.ModeMCP:
    return mcpserver.Serve(ctx, mind.Sandbox, mind.WebSearch, mind.Symbols)
  case input.ModeLSP:
    return runLSP(ctx, opts, mind)
  case input.ModeWeb:
    return runWeb(ctx, opts, mind)
  case input.ModeLibrary:
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
    fmt.Println(ui.Dim("resume with: armin -resume " + repl.ShortID(r.Meta.ID)))
  }

  return nil
}
