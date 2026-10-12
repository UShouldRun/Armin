package repl

import (
  "bufio"
  "context"
  "errors"
  "fmt"
  "io"
  "os"
  "strings"
  "time"

  "armin/internal/share"
  "armin/internal/ui"
)

// DialAttached joins the project's armin instance, started in root
// or a parent of it, or fails fast when there is none.
func DialAttached(root string) (*Attached, error) {
  c, err := share.Dial(root, "terminal")
  if err != nil {
    return nil, err
  }

  return &Attached{
    client: c,
    events: make(chan share.Event, 1024),
    wake:   make(chan struct{}, 1),
  }, nil
}

// Interrupt stops the turn in flight, if one is: Ctrl-C's handler.
func (a *Attached) Interrupt() bool {
  if !a.inTurn.Load() {
    return false
  }

  a.client.Stop()

  return true
}

// pump passes the host's events on; another frontend starting a
// turn or clearing wakes the prompt so its output can show.
func (a *Attached) pump() {
  defer close(a.events)

  for e := range a.client.Events {
    a.events <- e
    wakes := e.Type == "user" || e.Type == "cleared" || e.Type == "model"
    if wakes && e.Origin != a.client.Origin {
      select {
      case a.wake <- struct{}{}:
      default:
      }
    }
  }
}

// Run is the joined terminal's loop, until /quit, EOF or the host
// going away.
func (a *Attached) Run(ctx context.Context) error {
  defer a.client.Close()
  go a.pump()

  c := a.client
  ui.Banner(a.root(), c.Model.Current, "shared")
  ui.Instance(c.Model.Current, c.Instance, "— joined, shared with nvim and the other armins here (armin -new for a separate one)")
  if len(c.History) > 0 {
    printHistory(c.History)
  }

  a.keys = ui.NewKeyPump(bufio.NewReader(os.Stdin))
  pump := a.keys
  partial := ""
  for {
    // A wake that came in while output ran is already handled.
    select {
    case <-a.wake:
    default:
    }

    if !a.drain() {
      return nil
    }

    input, err := ui.ReadLineWake(ctx, pump, "\n" + ui.Prompt(), partial, a.wake, nil)
    partial = ""
    switch {
    case errors.Is(err, ui.ErrWoken):
      partial = input
      if !a.follow("") {
        return nil
      }

      continue
    case errors.Is(err, ui.ErrInterrupted):
      fmt.Println()
      continue
    case errors.Is(err, io.EOF), errors.Is(err, context.Canceled):
      return nil
    case err != nil:
      return fmt.Errorf("\n\t... in Run:\n\t input error: %w", err)
    }

    input = strings.TrimSpace(input)
    if input == "" {
      continue
    }

    if strings.HasPrefix(input, "/") {
      if !a.command(input) {
        return nil
      }

      continue
    }

    if c.Ask(input, "") != nil {
      return share.ErrGone
    }

    if !a.follow(c.Origin) {
      return nil
    }
  }
}

// drain shows events that arrived while nothing was following —
// a clear from nvim, say. False once the connection is gone.
func (a *Attached) drain() bool {
  for {
    select {
    case e, ok := <-a.events:
      if !ok {
        ui.Error("%s", share.ErrGone)
        return false
      }

      a.note(e)
    default:
      return true
    }
  }
}

// follow renders one turn, from its "user" event to "done" or
// "error". origin is who asked, "" for whoever's turn starts next.
// False once the connection is gone.
func (a *Attached) follow(origin string) bool {
  a.inTurn.Store(true)
  defer a.inTurn.Store(false)

  v := &turnView{
    wrap:    ui.NewWrapper(ui.TextWidth()),
    tips:    attachedTips,
    tipBase: tipTurn.Add(1),
    start:   time.Now(),
  }

  v.setPhase(phaseWaiting, true)
  v.startSpinner()
  defer v.stopSpinner()

  for e := range a.events {
    if origin == "" {
      switch e.Type {
      case "user":
        origin = e.Origin
        v.stopSpinner()
        fmt.Println(ui.Dim("  " + e.Origin + " asked:"))
        fmt.Println(ui.Prompt() + e.Delta)
        v.startSpinner()

        continue
      case "cleared", "model":
        v.stopSpinner()
        a.note(e)

        return true
      }

      continue
    }

    if e.Origin != origin {
      continue
    }

    switch e.Type {
    case "token":
      v.token(e.Delta)
    case "reasoning":
      v.setPhase(phaseThinking, true)
      v.startSpinner()
    case "tool_call":
      v.toolCall(e.Name, e.Args)
    case "tool_result":
      v.toolSummary(e.Delta)
    case "notice":
      v.stopSpinner()
      v.endLine()
      ui.Info("context: %s", e.Delta)
      v.startSpinner()
    case "done":
      v.stopSpinner()
      if v.streamed {
        v.flush()
        fmt.Println()
      } else if e.Delta != "" {
        v.endLine()
        fmt.Println(ui.RenderMarkdown(e.Delta, ui.TextWidth()))
      }

      return true
    case "error":
      v.stopSpinner()
      v.endLine()
      if e.Delta == "stopped" {
        ui.Info("interrupted")
      } else {
        ui.Error("%s", e.Delta)
      }

      return true
    }
  }

  v.stopSpinner()
  v.endLine()
  ui.Error("%s", share.ErrGone)

  return false
}

// note shows what another frontend changed between turns.
func (a *Attached) note(e share.Event) {
  if e.Origin == a.client.Origin {
    return
  }

  switch e.Type {
  case "cleared":
    ui.Info("conversation cleared from %s", e.Origin)
  case "model":
    if e.Model != nil {
      ui.Instance(e.Model.Current, a.client.Instance, "— switched")
    }
  }
}

// root is the project the host serves, for the banner.
func (a *Attached) root() string {
  wd, err := os.Getwd()
  if err != nil {
    return "."
  }

  return wd
}

// command runs a slash command; false means leave.
func (a *Attached) command(input string) bool {
  name, arg, _ := strings.Cut(strings.TrimPrefix(input, "/"), " ")
  arg = strings.TrimSpace(arg)

  switch name {
  case "quit", "exit", "q":
    return false

  case "tips":
    printTips(attachedTips)

  case "help", "?":
    for _, l := range []string{
      "/clear          start the shared conversation over",
      "/model [name]   list models, or switch the instance to one",
      "/history        reprint the conversation",
      "/tips           list tips",
      "/quit           leave (the instance keeps running)",
    } {
      fmt.Println("  " + l)
    }

  case "clear":
    err := a.client.Clear(context.Background())
    if err != nil {
      ui.Error("%s", err)
    }

  case "history":
    lines, err := a.client.Lines(context.Background())
    if err != nil {
      ui.Error("%s", err)
      return true
    }

    printHistory(lines)

  case "model":
    if arg != "" && arg == a.client.Model.Hosted {
      info, err := a.client.Models(context.Background())
      if err == nil {
        arg = ChooseHosted(info, arg, a.ask)
      }

      if arg == "" {
        return true
      }
    }

    if arg != "" {
      ui.Info("switching to %s…", arg)
    }

    info, err := a.client.SwitchModel(context.Background(), arg)
    if err != nil {
      ui.Error("%s", err)
      return true
    }

    printModels(&info, arg != "")

  default:
    ui.Error("unknown command /%s — /help lists them", name)
  }

  return true
}

func printHistory(h []share.ChatLine) {
  if len(h) == 0 {
    ui.Info("the conversation is empty")
    return
  }

  for _, l := range h {
    switch l.Role {
    case "user":
      fmt.Println("\n" + ui.Prompt() + l.Text)
    case "tool":
      ui.ToolLine(l.Text)
    default:
      fmt.Println(ui.RenderMarkdown(l.Text, ui.TextWidth()))
    }
  }
}

func printModels(m *share.ModelInfo, switched bool) {
  if m == nil {
    return
  }

  if switched {
    ui.Info("now using %s", m.Current)
    return
  }

  printModelInfo(*m)
}

// ask reads one answer through the key pump.
func (a *Attached) ask(prompt string) (string, error) {
  return ui.ReadLineWake(context.Background(), a.keys, prompt, "", nil, nil)
}
