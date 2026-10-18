// Models: /model shows and switches the chat model; printModelInfo
// and ChooseHosted show OpenRouter's suggestions with prices.

package repl

import (
  "context"
  "fmt"
  "strconv"
  "strings"

  "armin/internal/agent"
  "armin/internal/catalog"
  "armin/internal/share"
  "armin/internal/ui"
)

// printModelInfo is /model's listing: the configured models, the
// hosted ones worth trying, and how to switch.
func printModelInfo(info share.ModelInfo) {
  width := 0
  for _, c := range info.Configured {
    width = max(width, len(c.Name))
  }

  fmt.Println(ui.Dim("configured:"))
  for _, c := range info.Configured {
    marker := "  "
    if c.Name == info.Current {
      marker = ui.Green("→ ")
    }

    state := " (local, free · stopped, spawns on use)"
    switch {
    case !c.Managed && c.Cost != nil:
      state = " (hosted · " + costLabel(*c.Cost) + ")"
    case !c.Managed:
      state = " (hosted)"
    case c.Running:
      state = " (local, free · running)"
    }

    fmt.Printf("%s%-*s  %s%s\n", marker, width, c.Name, ui.Dim(c.Model), ui.Dim(state))
  }

  if info.Completion != "" && info.Completion != info.Current {
    fmt.Println(ui.Dim("  completion stays on " + info.Completion + ", on this machine"))
  }

  if info.Hosted != "" {
    printSuggestions(info)
  }

  if info.Balance != nil {
    fmt.Println(balanceLine(*info.Balance))
  }

  help := "/model <name> switch · /model local back"
  if info.Hosted != "" {
    help = "/model <name> switch · /model " + info.Hosted + ":<id> any OpenRouter model · /model local back"
  }

  fmt.Println(ui.Dim("─ " + help + " · completion stays local"))
}

// printSuggestions lists the catalog's picks as switchable names
// with what they cost.
func printSuggestions(info share.ModelInfo) {
  if len(info.Suggested) == 0 {
    fmt.Println(ui.Dim("suggestions: loading from OpenRouter — /model again in a moment"))
    return
  }

  width := 0
  for _, s := range info.Suggested {
    width = max(width, len(s.ID))
  }

  fmt.Printf(
    "%s\n",
    ui.Dim(fmt.Sprintf(
      "suggested on OpenRouter — /model %s:<id>; $ in / out per 1M tokens, ~ a deep session (%.1fM in, %dk out):",
      info.Hosted, float64(catalog.SessionIn) / 1e6, catalog.SessionOut / 1000,
    )),
  )

  free := false
  for _, s := range info.Suggested {
    if s.Free && !free {
      free = true
      fmt.Println(ui.Dim("  free (rate-limited; check the provider's data policy):"))
    }

    marker := "  "
    if info.Hosted + ":" + s.ID == info.Current {
      marker = ui.Green("→ ")
    }

    price := costLabel(s)

    fmt.Printf("%s%-*s  %s  %s\n", marker, width, s.ID, ui.Dim(price), ui.Dim(contextLabel(s.Context)))
  }
}

func contextLabel(n int) string {
  switch {
  case n >= 1_000_000:
    return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n) / 1e6), ".0") + "M ctx"
  case n > 0:
    return fmt.Sprintf("%dk ctx", n / 1000)
  }

  return ""
}

// costLabel is a hosted model's price: per 1M tokens in / out, and a
// deep session (see catalog.SessionIn).
func costLabel(s catalog.Suggestion) string {
  if s.In == 0 && s.Out == 0 {
    return "free"
  }

  return fmt.Sprintf("$%.2f / $%.2f per 1M · ~$%.2f a session", s.In, s.Out, s.Session)
}

// balanceLine is what's left on the OpenRouter account.
func balanceLine(b catalog.Balance) string {
  left := b.Credits - b.Used
  line := fmt.Sprintf(
    "OpenRouter: $%.2f left of $%.2f · today $%.2f", left, b.Credits, b.Today,
  )
  if b.KeyLimit > 0 {
    line += fmt.Sprintf(" · this key: $%.2f of its $%.2f cap left", b.KeyLeft, b.KeyLimit)
  }

  if left < 1 {
    return ui.Yellow(line)
  }

  return ui.Bold(line)
}

// ChooseHosted asks which OpenRouter model to use through hosted
// (a configured model like "planner"): Enter keeps its own id, a
// number picks a suggestion, anything with a "/" is any OpenRouter
// id. It returns the name to switch to, "" when nothing was chosen.
func ChooseHosted(info share.ModelInfo, hosted string, read func(string) (string, error)) string {
  def := ""
  var defCost *catalog.Suggestion
  for _, c := range info.Configured {
    if c.Name == hosted {
      def, defCost = c.Model, c.Cost
    }
  }

  fmt.Println(ui.Bold("OpenRouter model for " + hosted) + ui.Dim(" — Enter keeps the default"))

  width := len(def)
  for _, s := range info.Suggested {
    width = max(width, len(s.ID))
  }

  cost := ""
  if defCost != nil {
    cost = costLabel(*defCost)
  }

  fmt.Printf("  %s %-*s  %s  %s\n", ui.Green("→"), width, def, ui.Dim(cost), ui.Dim("(default)"))
  for i, s := range info.Suggested {
    fmt.Printf("  %s %-*s  %s\n", ui.Dim(fmt.Sprintf("%d", i + 1)), width, s.ID, ui.Dim(costLabel(s)))
  }

  if len(info.Suggested) == 0 {
    fmt.Println(ui.Dim("  (suggestions still loading — type any OpenRouter id, or Enter)"))
  }

  for {
    answer, err := read(ui.Yellow("model") + ui.Dim(" [Enter = " + def + ", a number, or an OpenRouter id]: "))
    if err != nil {
      return ""
    }

    answer = strings.TrimSpace(answer)
    n, numErr := strconv.Atoi(answer)
    switch {
    case answer == "" || answer == def:
      return hosted
    case numErr == nil && n >= 1 && n <= len(info.Suggested):
      return hosted + ":" + info.Suggested[n - 1].ID
    case strings.Contains(answer, "/"):
      return hosted + ":" + answer
    }

    ui.Error("not a number from the list or an OpenRouter id (vendor/model)")
  }
}

func cmdModel(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    printCurrentModel(r)
    return
  }

  name := args[0]

  // A bare OpenRouter model asks which model to reach through it.
  info := r.Host().Models()
  if name == info.Hosted && r.pump != nil {
    name = ChooseHosted(info, name, r.ask)
    if name == "" {
      return
    }
  }

  r.switchModel(ctx, name)
}

// switchModel switches chat (and completion, between local models)
// to name, for this terminal or for a frontend that joined it.
func (r *REPL) switchModel(ctx context.Context, name string) {
  err := r.Models.SwitchChat(ctx, r.LLM, r.Follower, name)
  if err != nil {
    ui.Error("switching to %q: %s", name, err)
    return
  }

  if r.ContextFromServer {
    n, err := r.LLM.ContextSize(ctx)
    if err != nil {
      n = 0
    }

    r.Loop.ContextSize = n
  }

  // A nil *llamacpp.Window must become a nil interface, not a typed nil.
  r.Loop.Window = nil
  w := r.Models.Window(name)
  if w != nil {
    r.Loop.Window = w
  }

  agent.SetModelLine(r.History, name, r.Models.Label(name))
  r.lastTokens = 0
  r.current.Store(&name)
  completing := r.completer().Model
  r.completing.Store(&completing)
  ui.Info("switched to %s", r.modelName())
  if completing != name {
    ui.Info("completion stays on %s, on this machine", completing)
  }

  info := r.Host().Models()
  r.emit(share.Event{ Type: "model", Model: &info })
}

func printCurrentModel(r *REPL) {
  printModelInfo(r.Host().Models())
}
