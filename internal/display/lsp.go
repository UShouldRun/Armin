// -lsp: nvim's frontend. runLSP serves it from this armin (and lets
// later terminals join); joinLSP serves it from the project's running
// instance, keeping only a local symbol index.

package display

import (
  "context"
  "fmt"
  "os"

  "armin/internal/input"
  "armin/internal/lspserver"
  "armin/internal/share"
  "armin/internal/think"
)

// runLSP serves -lsp on stdio, and the chat to terminals started in
// the same project (see lspserver.ListenAttach).
func runLSP(ctx context.Context, opts *input.Options, mind *think.Mind) error {
  mind.Loop.ReadOnly = true

  server := lspserver.New(
    mind.Loop, mind.Sandbox, mind.SystemPrompt(true),
    mind.Completion, mind.Models, mind.Symbols,
  )
  server.Follow(mind.Follower())

  // The first armin in a project is its instance (see Join).
  hub, err := share.Listen(ctx, mind.Sandbox.Root, server)
  if err == nil {
    server.Share(hub)
    defer hub.Close()
  }

  err = server.Serve(ctx, os.Stdin, os.Stdout)
  if err != nil {
    return fmt.Errorf("\n\t... in runLSP:\n\t%w", err)
  }

  return nil
}

// joinLSP serves nvim from the project's instance: symbols from a
// local ctags index, everything that thinks from host.
func joinLSP(ctx context.Context, opts *input.Options, host *share.Client) error {
  mind, err := think.NewSenses(opts)
  if err != nil {
    return fmt.Errorf("\n\t... in joinLSP:\n\t%w", err)
  }

  defer mind.Shutdown()

  server := lspserver.NewProxy(mind.Sandbox, mind.Symbols, host)
  err = server.Serve(ctx, os.Stdin, os.Stdout)
  if err != nil {
    return fmt.Errorf("\n\t... in joinLSP:\n\t%w", err)
  }

  return nil
}
