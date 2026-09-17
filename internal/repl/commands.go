package repl

import (
  "context"
  "fmt"
  "sort"
  "strconv"
  "strings"

  "armin/internal/agent"
  "armin/internal/convo"
  "armin/internal/llm"
  "armin/internal/session"
  "armin/internal/ui"
)

type command struct {
  names   []string // first entry is canonical, rest are aliases
  usage   string
  summary string
  run     func(r *REPL, ctx context.Context, args []string)
}

// commandList is a function, not a package var, on purpose: cmdHelp
// below needs the full list, and a package var literal holding
// cmdHelp while cmdHelp reads that same var is an initialization
// cycle as far as the compiler's concerned, even though nothing
// calls cmdHelp until well after init.
func commandList() []command {
  return []command{
    {
      names:   []string{ "help", "?" },
      summary: "list commands",
      run:     cmdHelp,
    },
    {
      names:   []string{ "quit", "exit", "q" },
      summary: "leave armin",
      run:     cmdQuit,
    },
    {
      names:   []string{ "clear" },
      summary: "reset the conversation, keeping only the system prompt",
      run:     cmdClear,
    },
    {
      names:   []string{ "model" },
      usage:   "/model [name]",
      summary: "show or switch the active model",
      run:     cmdModel,
    },
    {
      names:   []string{ "context", "ctx" },
      summary: "show context usage for this session",
      run:     cmdContext,
    },
    {
      names:   []string{ "compact" },
      summary: "summarize older history to free up context",
      run:     cmdCompact,
    },
    {
      names:   []string{ "tools" },
      summary: "list the tools available to the model",
      run:     cmdTools,
    },
    {
      names:   []string{ "root" },
      summary: "show the sandbox's project root and safety settings",
      run:     cmdRoot,
    },
    {
      names:   []string{ "history" },
      summary: "dump the raw conversation history",
      run:     cmdHistory,
    },
    {
      names:   []string{ "save" },
      summary: "save the current session to disk now",
      run:     cmdSave,
    },
    {
      names:   []string{ "suggest" },
      summary: "toggle follow-up question suggestions after each answer",
      run:     cmdSuggest,
    },
    {
      names:   []string{ "ghosttext" },
      summary: "toggle live ghost-text completion while you type",
      run:     cmdGhostText,
    },
  }
}

var byName = buildCommandIndex()

func buildCommandIndex() map[string]*command {
  list := commandList()

  idx := make(map[string]*command, len(list))
  for i := range list {
    c := &list[i]
    for _, name := range c.names {
      idx[name] = c
    }
  }
  return idx
}

// dispatch parses a leading-"/" line and runs the matching command,
// or prints a hint if there isn't one — never silently drops a
// typo.
func (r *REPL) dispatch(ctx context.Context, input string) {
  fields := strings.Fields(input)
  name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
  args := fields[1:]

  c, ok := byName[name]
  if !ok {
    ui.Error("unknown command /%s — try /help", name)
    return
  }
  c.run(r, ctx, args)
}

func cmdQuit(r *REPL, _ context.Context, _ []string) {
  r.state |= flagQuit
}

func cmdHelp(r *REPL, _ context.Context, _ []string) {
  fmt.Println(ui.Bold("commands"))

  for _, c := range commandList() {
    usage := c.usage
    if usage == "" {
      usage = "/" + c.names[0]
    }
    fmt.Printf("  %-18s %s\n", usage, ui.Dim(c.summary))

    if len(c.names) > 1 {
      aliases := "aliases: /" + strings.Join(c.names[1:], ", /")
      fmt.Printf("  %-18s %s\n", "", ui.Dim(aliases))
    }
  }
}

func cmdClear(r *REPL, _ context.Context, _ []string) {
  if len(r.History) == 0 {
    return
  }
  r.History = r.History[:1] // keep the system prompt
  resyncSession(r)
  ui.Info("conversation cleared")
}

// resyncSession does an immediate full session rewrite and resets
// the append-cursor, for the rare commands that mutate History in
// place (rather than only appending to it) — /clear and /compact.
// Without this, the next autosave's Append would either miss the
// rewrite entirely (if persisted still exceeds the new, shorter
// History) or append onto a log that no longer matches in-memory
// state.
func resyncSession(r *REPL) {
  if r.state&flagAutosave == 0 {
    r.persisted = 0
    return
  }
  if err := session.Save(r.Root, r.LLM.Model, r.History); err != nil {
    ui.Error("failed to resync session: %s", err)
    return
  }
  r.persisted = len(r.History)
}

func cmdModel(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    printCurrentModel(r, ctx)
    return
  }

  name := args[0]
  models, err := r.LLM.ListModels(ctx)
  if err == nil && !hasModel(models, name) {
    ui.Info(
      "%q isn't in ollama's installed model list — switching "+
        "anyway; \"ollama pull %s\" first if the next call fails",
      name, name,
    )
  }

  r.LLM.Model = name
  ui.Info("switched to %s", name)
}

func printCurrentModel(r *REPL, ctx context.Context) {
  fmt.Printf("current model: %s\n", ui.Bold(r.LLM.Model))

  models, err := r.LLM.ListModels(ctx)
  if err != nil {
    ui.Info("(couldn't reach ollama to list installed models: %s)", err)
    return
  }

  fmt.Println(ui.Dim("installed:"))
  for _, m := range models {
    marker := "  "
    if m.Name == r.LLM.Model {
      marker = ui.Green("→ ")
    }
    fmt.Printf("%s%s\n", marker, m.Name)
  }
}

func hasModel(models []llm.TagsModel, name string) bool {
  for _, m := range models {
    if m.Name == name {
      return true
    }
  }
  return false
}

func cmdContext(r *REPL, _ context.Context, _ []string) {
  s := convo.Analyze(r.History)
  tokenNote := fmt.Sprintf(
    "~%d (rough estimate, not a real tokenizer)", s.ApproxTokens,
  )

  fmt.Println(ui.Bold("context"))
  fmt.Printf("  messages:       %d\n", s.Messages)
  fmt.Printf("  tool calls:     %d\n", s.ToolCalls)
  fmt.Printf("  tool results:   %d\n", s.ToolResults)
  fmt.Printf("  approx tokens:  %s\n", ui.Dim(tokenNote))
  fmt.Println(ui.Dim(
    "  /compact summarizes everything but the last few messages " +
      "to free this up",
  ))
}

func cmdCompact(r *REPL, ctx context.Context, args []string) {
  keep := compactKeepLast
  if len(args) > 0 {
    n, err := strconv.Atoi(args[0])
    if err != nil {
      ui.Error("expected a number of messages to keep, got %q", args[0])
      return
    }
    keep = n
  }

  before := convo.Analyze(r.History)

  spinner := ui.NewSpinner("compacting")
  err := convo.Compact(ctx, r.LLM, &r.History, keep)
  spinner.Stop()

  if err != nil {
    ui.Error("%s", err)
    return
  }
  resyncSession(r)

  after := convo.Analyze(r.History)
  ui.Info(
    "compacted: %d messages (~%d tokens) → %d messages (~%d tokens)",
    before.Messages, before.ApproxTokens,
    after.Messages, after.ApproxTokens,
  )
}

func cmdTools(r *REPL, _ context.Context, _ []string) {
  fmt.Println(ui.Bold("tools"))
  for _, t := range agent.ToolDefinitions() {
    fmt.Printf(
      "  %-16s %s\n", t.Function.Name, ui.Dim(t.Function.Description),
    )
  }
}

func cmdRoot(r *REPL, _ context.Context, _ []string) {
  fmt.Println(ui.Bold("sandbox"))
  fmt.Printf("  root:        %s\n", r.Sandbox.Root)
  fmt.Printf("  timeout:     %s\n", r.Sandbox.Timeout)
  fmt.Printf("  max output:  %d bytes\n", r.Sandbox.MaxOutput)
  fmt.Printf("  dry run:     %v\n", r.Sandbox.DryRun)

  allowed := make([]string, 0, len(r.Sandbox.Allowlist))
  for b := range r.Sandbox.Allowlist {
    allowed = append(allowed, b)
  }
  sort.Strings(allowed)
  fmt.Printf("  allowlist:   %s\n", strings.Join(allowed, ", "))
}

func cmdHistory(r *REPL, _ context.Context, _ []string) {
  for i, m := range r.History {
    preview := strings.ReplaceAll(m.Content, "\n", " ")
    if len(preview) > 100 {
      preview = preview[:100] + "…"
    }
    fmt.Printf("  %3d  %-9s %s\n", i, m.Role, ui.Dim(preview))
  }
}

func cmdSave(r *REPL, _ context.Context, _ []string) {
  if err := session.Save(r.Root, r.LLM.Model, r.History); err != nil {
    ui.Error("%s", err)
    return
  }
  // A full Save just wrote everything — keep the append-cursor in
  // sync, or the next autosave would redundantly re-append messages
  // this Save already wrote, duplicating lines in the log.
  r.persisted = len(r.History)
  ui.Info("session saved")
}

func cmdSuggest(r *REPL, _ context.Context, _ []string) {
  r.state ^= flagSuggest
  if r.state&flagSuggest != 0 {
    ui.Info("suggestions on — pick one after an answer by typing its number")
    return
  }
  r.lastSuggestions = nil
  ui.Info("suggestions off")
}

func cmdGhostText(r *REPL, _ context.Context, _ []string) {
  r.state ^= flagGhostText
  if r.state&flagGhostText != 0 {
    ui.Info(
      "ghost text on — Tab accepts a suggestion; note this trades " +
        "away arrow-key editing and history while typing a line",
    )
    return
  }
  ui.Info("ghost text off")
}
