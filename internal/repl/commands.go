// Slash commands: the table (name, usage, summary, handler), dispatch,
// /help, and the small ones: toggles, /context, /compact, /tools,
// /root, /add-dir. Bigger groups have their own files.

package repl

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strconv"
  "strings"

  "armin/internal/agent"
  "armin/internal/convo"
  "armin/internal/llm"
  "armin/internal/ui"
)

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
      names:   []string{ "tips" },
      summary: "list tips about armin's commands",
      run:     cmdTips,
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
    {
      names:   []string{ "thinking" },
      summary: "toggle showing the model's thinking as it streams",
      run:     cmdThinking,
    },
    {
      names:   []string{ "approve" },
      summary: "toggle asking before each file write",
      run:     cmdApprove,
    },
    {
      names:   []string{ "memory", "mem" },
      usage:   "/memory [show|rm <name>]",
      summary: "list, show or delete memories",
      run:     cmdMemory,
    },
    {
      names:   []string{ "remember" },
      usage:   "/remember <text>",
      summary: "ask the model to save something to memory",
      run:     cmdRemember,
    },
    {
      names:   []string{ "skills" },
      summary: "list available skills",
      run:     cmdSkills,
    },
    {
      names:   []string{ "skill" },
      usage:   "/skill <name> [task]",
      summary: "run a skill (also: /<name> [task])",
      run:     cmdSkill,
    },
    {
      names:   []string{ "add-dir" },
      usage:   "/add-dir <dir>",
      summary: "let armin read a directory outside the project (read-only)",
      run:     cmdAddDir,
    },
    {
      names:   []string{ "sessions" },
      usage:   "/sessions [rm <id>]",
      summary: "list saved sessions (* = this directory), or delete one and its scratchpad",
      run:     cmdSessions,
    },
    {
      names:   []string{ "resume" },
      usage:   "/resume <id>",
      summary: "switch to a saved session of this directory",
      run:     cmdResume,
    },
    {
      names:   []string{ "title" },
      usage:   "/title <text>",
      summary: "rename this session",
      run:     cmdTitle,
    },
    {
      names:   []string{ "effort" },
      usage:   "/effort [auto|off|low|medium|high] [task]",
      summary: "how hard the model thinks; auto lets it judge " +
        "each request. With a task, for that task only",
      run:     cmdEffort,
    },
    {
      names:   []string{ "library", "lib" },
      usage: "/library [index|catalog [model]|merge [model]|" +
        "add <path>... [to <folder>]|topics [topic]]",
      summary: "the reference library: status, reindex, " +
        "catalog topics, add files",
      run:     cmdLibrary,
    },
    {
      names:   []string{ "scratch" },
      usage:   "/scratch [show <name>|clear]",
      summary: "list, show or clear the scratchpad",
      run:     cmdScratch,
    },
    {
      names:   []string{ "trace" },
      summary: "expand the last error's function trace",
      run:     cmdTrace,
    },
  }
}

// cmdTrace prints the full chain of the last error shown, which
// Error collapsed to its message.
func cmdTrace(r *REPL, ctx context.Context, args []string) {
  ui.ShowTrace()
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
  defer r.snapshot() // /clear, /compact, /resume rewrite History
  fields := strings.Fields(input)
  name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
  args := fields[1:]

  c, ok := byName[name]
  if ok {
    c.run(r, ctx, args)
    return
  }

  if r.Loop.Skills != nil {
    _, ok := r.Loop.Skills.Get(name)
    if ok {
      cmdSkill(r, ctx, append([]string{ name }, args...))
      return
    }
  }

  ui.Error("unknown command /%s — try /help", name)
}

func cmdQuit(r *REPL, _ context.Context, _ []string) {
  r.state |= flagQuit
}

func cmdHelp(r *REPL, _ context.Context, _ []string) {
  fmt.Println(ui.Bold("commands"))

  list := commandList()
  width := 0
  for _, c := range list {
    width = max(width, len(usageOf(c)))
  }

  for _, c := range list {
    fmt.Printf("  %-*s  %s\n", width, usageOf(c), ui.Dim(c.summary))

    if len(c.names) > 1 {
      aliases := "aliases: /" + strings.Join(c.names[1:], ", /")
      fmt.Printf("  %-*s  %s\n", width, "", ui.Dim(aliases))
    }
  }
}

func usageOf(c command) string {
  if c.usage != "" {
    return c.usage
  }

  return "/" + c.names[0]
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
  if r.lastTokens > 0 {
    fmt.Printf("  last request:   %d tokens (server count)\n", r.lastTokens)
  }

  w := r.Loop.Window
  if w != nil {
    tier := fmt.Sprintf(
      "resident %dk (fast, all in VRAM); can grow to %dk",
      w.Resident() / 1000, w.Max() / 1000,
    )
    if w.Streaming() {
      tier = fmt.Sprintf(
        "streaming %dk (KV cache in RAM via ring buffer); "+
          "shrinks back to %dk after a compaction",
        w.Max() / 1000, w.Resident() / 1000,
      )
    }

    fmt.Printf("  tier:           %s\n", tier)
  }

  size := r.Loop.ContextSize
  if r.Loop.Window != nil {
    size = r.Loop.Window.Size()
  }

  if size > 0 {
    used := max(r.lastTokens, s.ApproxTokens)
    fmt.Printf(
      "  window:         %d tokens, %d%% used, auto-compacts at %d%%\n",
      size, used * 100 / size, agent.DefaultCompactAt,
    )
  } else {
    fmt.Println(ui.Dim("  window:         unknown — auto-compaction off (set context_size)"))
  }

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
  extra := ""
  if r.Loop.Scratch != nil {
    list := r.Loop.Scratch.Summary()
    if list != "" {
      extra = "Scratchpad files (scratchpad_read to open):\n" + list
    }
  }

  err := convo.Compact(ctx, r.LLM, &r.History, keep, extra)
  r.lastTokens = 0
  spinner.Stop()

  if err != nil {
    ui.Err(err)
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
  for _, t := range r.Loop.Definitions() {
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

func cmdSuggest(r *REPL, _ context.Context, _ []string) {
  r.state ^= flagSuggest
  if r.state & flagSuggest != 0 {
    ui.Info("suggestions on — pick one after an answer by typing its number")
    return
  }

  r.lastSuggestions = nil
  ui.Info("suggestions off")
}

func cmdGhostText(r *REPL, _ context.Context, _ []string) {
  r.state ^= flagGhostText
  if r.state & flagGhostText != 0 {
    ui.Info(
      "ghost text on — Tab accepts a suggestion; note this trades " +
        "away arrow-key editing and history while typing a line",
    )

    return
  }

  ui.Info("ghost text off")
}

func cmdThinking(r *REPL, _ context.Context, _ []string) {
  r.state ^= flagHideThinking
  if r.state & flagHideThinking != 0 {
    ui.Info("thinking hidden — the spinner shows while the model thinks")
    return
  }

  ui.Info("thinking shown, dimmed, as it streams")
}

func cmdApprove(r *REPL, _ context.Context, _ []string) {
  r.state ^= flagAutoApprove
  if r.state & flagAutoApprove != 0 {
    ui.Info("file writes apply without asking")
    return
  }

  ui.Info("armin asks before each file write")
}

// sessionRows caps /sessions and the startup menu.
const sessionRows = 25

func cmdAddDir(r *REPL, _ context.Context, args []string) {
  if len(args) == 0 {
    if len(r.Sandbox.ReadRoots) == 0 {
      ui.Info("no extra directories — /add-dir <dir> grants read access to one")
      return
    }

    for _, d := range r.Sandbox.ReadRoots {
      fmt.Println("  " + d)
    }

    return
  }

  err := r.AddDir(strings.Join(args, " "))
  if err != nil {
    ui.Err(err)
  }
}

// maxListedFiles caps the file list sent to the model on /add-dir.
const maxListedFiles = 40

// AddDir grants read access to dir and tells the model, listing what
// is there, so a request like "read the slides in the parent folder"
// has somewhere to go. Writes stay inside the project.
func (r *REPL) AddDir(dir string) error {
  if strings.HasPrefix(dir, "~/") {
    home, err := os.UserHomeDir()
    if err == nil {
      dir = filepath.Join(home, dir[2:])
    }
  } else if !filepath.IsAbs(dir) {
    dir = filepath.Join(r.Root, dir)
  }

  resolved, err := r.Sandbox.AddReadRoot(dir)
  if err != nil {
    return err
  }

  entries, _ := os.ReadDir(resolved)
  var names []string
  for _, e := range entries {
    if strings.HasPrefix(e.Name(), ".") {
      continue
    }

    name := e.Name()
    if e.IsDir() {
      name += "/"
    }

    names = append(names, name)
    if len(names) == maxListedFiles {
      names = append(names, "…")
      break
    }
  }

  r.History = append(r.History, llm.Message{
    Role: "user",
    Content: fmt.Sprintf(
      "[armin] I granted you read access to %s. It contains: %s. "+
        "Use absolute paths there (e.g. read_pdf / read_file with "+
        "path=%s/<file>). Writing stays inside the project.",
      resolved, strings.Join(names, ", "), resolved,
    ),
  })

  ui.Info("read access: %s (%d entries)", resolved, len(names))
  return nil
}
