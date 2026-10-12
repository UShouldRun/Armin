package display

import (
  "fmt"
  "io"
  "os"
  "strings"

  "armin/internal/catalog"
  "armin/internal/input"
  "armin/internal/repl"
  "armin/internal/share"
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
    return readLine(os.Stdin)
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

// readLine reads up to a newline a byte at a time: a buffered reader
// would keep keys meant for the prompt that starts next.
func readLine(r io.Reader) (string, error) {
  var b strings.Builder
  var c [1]byte
  for {
    n, err := r.Read(c[:])
    if n == 1 {
      if c[0] == '\n' {
        return b.String(), nil
      }

      b.WriteByte(c[0])
    }

    if err != nil {
      return b.String(), err
    }
  }
}
