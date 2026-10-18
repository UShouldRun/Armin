// Before connecting: when the chat model is an OpenRouter one, offer
// the current suggestions (internal/catalog) so the user can start on
// another model.

package display

import (
  "fmt"
  "os"
  "strings"

  "armin/internal/catalog"
  "armin/internal/input"
  "armin/internal/repl"
  "armin/internal/share"
  "armin/internal/ui"
)

// ChooseModel asks which OpenRouter model to start on when the
// chat model is an OpenRouter one, before anything connects: Enter
// keeps its own id. Only for the terminal, and only with one to ask.
func ChooseModel(opts *input.Options) {
  spec, ok := opts.Config.Models[opts.Model]
  hosted := ok && strings.Contains(spec.URL, "openrouter.ai")
  if !hosted || opts.Mode != input.ModeREPL || opts.Resumed != nil || !terminal() {
    return
  }

  catalog.Load() // yesterday's suggestions, from disk

  info := share.ModelInfo{
    Configured: []share.ModelChoice{ { Name: opts.Model, Model: spec.ID } },
    Suggested:  catalog.Suggestions(),
  }

  cost, ok := catalog.Price(spec.ID)
  if ok {
    info.Configured[0].Cost = &cost
  }

  name := repl.ChooseHosted(info, opts.Model, func(prompt string) (string, error) {
    fmt.Print(prompt)
    return ui.ReadLineUnbuffered(os.Stdin)
  })
  if name != "" {
    opts.Model = name
  }

  fmt.Println()
}

// terminal: someone is there to answer.
func terminal() bool {
  info, err := os.Stdin.Stat()
  return err == nil && info.Mode() & os.ModeCharDevice != 0
}
