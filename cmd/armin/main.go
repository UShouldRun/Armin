// Command armin is a coding assistant backed by local llama.cpp
// models (or cloud ones). It runs in three steps, one package each:
//
//   input    what you asked for: flags, config.conf, .env, signals
//   think    the mind: sandboxed tools, models, the agent loop, notes
//   display  how it's shown: the terminal chat, -web, -lsp for nvim,
//            -mcp, or a terminal joining nvim's chat
//
// The project root can be given as a bare positional argument
// ("armin .", "armin ~/code/myproject") instead of -root.
package main

import (
  "context"
  "fmt"
  "os"

  "armin/internal/display"
  "armin/internal/input"
  "armin/internal/think"
  "armin/internal/ui"
)

func main() {
  err := run()
  if err != nil {
    ui.Err(err)
    os.Exit(1)
  }
}

func run() error {
  opts, err := input.Parse()
  if err != nil {
    return fmt.Errorf("\n\t... in run:\n\t%w", err)
  }

  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()

  sig := input.WatchSignals(cancel)

  // -install loads no models: it sizes one up and downloads it.
  if opts.Mode == input.ModeInstall {
    return display.Install(ctx, opts)
  }

  // nvim's armin already thinks for this project: share its chat
  // instead of loading a second set of models on the same GPU.
  joined, err := display.Join(ctx, opts, sig)
  if joined {
    return err
  }

  // Starting on an OpenRouter model asks which one, first.
  display.ChooseModel(opts)

  mind, err := think.New(ctx, opts)
  if err != nil {
    return fmt.Errorf("\n\t... in run:\n\t%w", err)
  }

  defer mind.Shutdown()
  sig.OnTerminate(mind.Shutdown)

  return display.Run(ctx, opts, mind, sig)
}
