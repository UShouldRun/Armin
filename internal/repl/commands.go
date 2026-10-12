package repl

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "time"

  "armin/internal/agent"
  "armin/internal/convo"
  "armin/internal/llm"
  "armin/internal/session"
  "armin/internal/share"
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
      names:   []string{ "scratch" },
      usage:   "/scratch [show <name>|clear]",
      summary: "list, show or clear the scratchpad",
      run:     cmdScratch,
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

func cmdClear(r *REPL, _ context.Context, _ []string) {
  if len(r.History) == 0 {
    return
  }

  r.History = r.History[:1] // keep the system prompt
  r.lastTokens = 0
  resyncSession(r)
  ui.Info("conversation cleared")
  r.emit(share.Event{ Type: "cleared", Origin: originTerminal })
}

// resyncSession does an immediate full session rewrite and resets
// the append-cursor, for the rare commands that mutate History in
// place (rather than only appending to it) — /clear and /compact.
// Without this, the next autosave's Append would either miss the
// rewrite entirely (if persisted still exceeds the new, shorter
// History) or append onto a log that no longer matches in-memory
// state.
func resyncSession(r *REPL) {
  if r.state & flagAutosave == 0 {
    r.persisted = 0
    return
  }

  err := session.Save(&r.Meta, r.History)
  if err != nil {
    ui.Error("failed to resync session: %s", err)
    return
  }

  r.persisted = len(r.History)
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
  err := session.Save(&r.Meta, r.History)
  if err != nil {
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

func cmdMemory(r *REPL, _ context.Context, args []string) {
  mem := r.Loop.Memories
  if mem == nil {
    ui.Error("memory is unavailable this session")
    return
  }

  if len(args) >= 2 {
    switch args[0] {
    case "show":
      m, body, err := mem.Read(args[1])
      if err != nil {
        ui.Error("%s", err)
        return
      }

      fmt.Printf("%s %s\n", ui.Bold(m.Name), ui.Dim("(" + m.Scope.String() + ") " + m.Description))
      fmt.Println(strings.TrimSpace(body))

      return
    case "rm", "delete", "forget":
      m, err := mem.Delete(args[1])
      if err != nil {
        ui.Error("%s", err)
        return
      }

      ui.Info("forgot %s", m.Name)

      return
    }
  }

  list, err := mem.List()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if len(list) == 0 {
    ui.Info("no memories yet — /remember <text>, or the model saves its own")
    return
  }

  fmt.Println(ui.Bold("memories"))
  for _, m := range list {
    fmt.Printf("  %-24s %s\n", m.Name, ui.Dim("(" + m.Scope.String() + ") " + m.Description))
  }
}

func cmdRemember(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    ui.Error("usage: /remember <text>")
    return
  }

  prompt := "Save this to memory with memory_save (pick the scope: " +
    "user for my preferences, project for this codebase): " +
    strings.Join(args, " ")
  err := r.turn(ctx, prompt)
  if err != nil {
    ui.Error("%s", err)
  }

  r.maybeAutosave()
}

func cmdSkills(r *REPL, _ context.Context, _ []string) {
  if r.Loop.Skills == nil || r.Loop.Skills.Len() == 0 {
    ui.Info(
      "no skills — add <name>/SKILL.md under ~/.config/armin/skills " +
        "or .armin/skills in the project",
    )

    return
  }

  fmt.Println(ui.Bold("skills"))
  for _, sk := range r.Loop.Skills.List() {
    where := "user"
    if sk.Project {
      where = "project"
    }

    fmt.Printf("  %-20s %s\n", sk.Name, ui.Dim("(" + where + ") " + sk.Description))
  }
}

// cmdSkill puts the skill's instructions straight into the user
// message, so the model needn't spend a round on use_skill.
func cmdSkill(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    cmdSkills(r, ctx, nil)
    return
  }

  if r.Loop.Skills == nil {
    ui.Error("no skills loaded")
    return
  }

  sk, ok := r.Loop.Skills.Get(args[0])
  if !ok {
    ui.Error("no skill named %q — /skills lists them", args[0])
    return
  }

  body, err := sk.Instructions()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  task := strings.Join(args[1:], " ")
  if task == "" {
    task = "(no extra input — follow the skill as written)"
  }

  prompt := fmt.Sprintf(
    "Use the %q skill for this task: %s\n\n<skill name=%q>\n%s\n</skill>",
    sk.Name, task, sk.Name, strings.TrimSpace(body),
  )
  files := sk.Files()
  if len(files) > 0 {
    prompt += "\nSupporting files (use_skill with file): " +
      strings.Join(files, ", ")
  }

  err = r.turn(ctx, prompt)
  if err != nil {
    ui.Error("%s", err)
  }

  r.maybeAutosave()
}

func cmdScratch(r *REPL, _ context.Context, args []string) {
  pad := r.Loop.Scratch
  if pad == nil {
    ui.Error("the scratchpad is unavailable this session")
    return
  }

  if len(args) >= 1 && args[0] == "clear" {
    n, err := pad.Clear()
    if err != nil {
      ui.Error("%s", err)
      return
    }

    ui.Info("cleared %d scratch files", n)

    return
  }

  if len(args) >= 2 && args[0] == "show" {
    content, _, err := pad.Read(args[1], 0, 0)
    if err != nil {
      ui.Error("%s", err)
      return
    }

    fmt.Print(content)

    return
  }

  list, err := pad.List()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if len(list) == 0 {
    ui.Info("scratchpad is empty (%s)", pad.Dir)
    return
  }

  fmt.Println(ui.Bold("scratchpad") + ui.Dim(" " + pad.Dir))
  for _, f := range list {
    fmt.Printf(
      "  %-24s %s\n", f.Name,
      ui.Dim(fmt.Sprintf("%d lines, %s", f.Lines, f.Modified.Format("Jan 2 15:04"))),
    )
  }
}

// sessionRows caps /sessions and the startup menu.
const sessionRows = 25

func cmdSessions(r *REPL, _ context.Context, args []string) {
  if len(args) >= 1 && (args[0] == "rm" || args[0] == "delete") {
    if len(args) < 2 {
      ui.Error("usage: /sessions rm <id>")
      return
    }

    r.deleteSession(args[1])

    return
  }

  list, err := session.List()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if len(list) == 0 {
    ui.Info("no saved sessions yet")
    return
  }

  fmt.Println(ui.Bold("sessions"))
  PrintSessions(list, r.Root, r.Meta.ID, false)
}

// PrintSessions renders a session table: directory, title, short id,
// age and size; * marks root's sessions, → the current one. numbered
// adds menu numbers.
func PrintSessions(list []session.Meta, root, current string, numbered bool) {
  if len(list) > sessionRows {
    list = list[:sessionRows]
  }

  dirW, titleW := 0, 0
  for _, m := range list {
    dirW = max(dirW, len(filepath.Base(m.Root)))
    titleW = max(titleW, min(len(titleOr(m.Title)), 44))
  }

  for i, m := range list {
    mark := "  "
    switch {
    case m.ID == current:
      mark = ui.Green("→ ")
    case m.Root == root:
      mark = ui.Cyan("* ")
    }

    num := ""
    if numbered {
      num = fmt.Sprintf("%2d  ", i + 1)
    }

    title := titleOr(m.Title)
    if len(title) > 44 {
      title = title[:43] + "…"
    }

    fmt.Printf(
      "  %s%s%-*s  %-*s  %s  %s\n",
      mark, num, dirW, filepath.Base(m.Root), titleW, title,
      ui.Dim(ShortID(m.ID)),
      ui.Dim(fmt.Sprintf("%s · %d msgs", ago(m.UpdatedAt), m.Messages)),
    )
  }
}

// ago renders a time as a short age: 5m, 3h, 2d, Jan 2.
func ago(t time.Time) string {
  d := time.Since(t)
  switch {
  case d < time.Hour:
    return fmt.Sprintf("%dm ago", max(int(d.Minutes()), 1))
  case d < 24 * time.Hour:
    return fmt.Sprintf("%dh ago", int(d.Hours()))
  case d < 7 * 24 * time.Hour:
    return fmt.Sprintf("%dd ago", int(d.Hours() / 24))
  }

  return t.Format("Jan 2")
}

// deleteSession removes a saved session and its scratchpad folder,
// after asking. Memories stay: they belong to the project.
func (r *REPL) deleteSession(id string) {
  saved, err := session.Load(id)
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if saved.ID == r.Meta.ID {
    ui.Error("that's the session you're in — /clear starts over, or delete it from another session")
    return
  }

  detail := fmt.Sprintf(
    "(%s, %s, %d msgs) and its scratchpad? [y/N]",
    ShortID(saved.ID), filepath.Base(saved.Root), saved.Messages,
  )
  answer, _ := r.ask(fmt.Sprintf("%s %s %s ", ui.Yellow("delete"), titleOr(saved.Title), ui.Dim(detail)))
  a := strings.ToLower(strings.TrimSpace(answer))
  if a != "y" && a != "yes" {
    ui.Info("kept")
    return
  }

  m, err := session.Delete(saved.ID)
  if err != nil {
    ui.Error("%s", err)
    return
  }

  scratch := "no scratchpad"
  if isUUID(m.ID) {
    dir, err := session.ProjectDataDir("scratch", m.Root)
    if err == nil {
      err := os.RemoveAll(filepath.Join(dir, m.ID))
      if err == nil {
        scratch = "its scratchpad"
      }
    }
  }

  ui.Info("deleted %s (%s) and %s", titleOr(m.Title), ShortID(m.ID), scratch)
}

func cmdResume(r *REPL, _ context.Context, args []string) {
  if len(args) == 0 {
    ui.Error("usage: /resume <id> — /sessions lists them")
    return
  }

  saved, err := session.Load(args[0])
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if saved.Root != r.Root {
    ui.Error(
      "that session belongs to %s — run: armin -resume %s",
      saved.Root, ShortID(saved.ID),
    )

    return
  }

  // Keep today's system prompt (memories, skills, model line).
  if len(saved.History) > 0 && saved.History[0].Role == "system" {
    saved.History[0] = r.History[0]
  }

  r.History = saved.History
  r.Meta = saved.Meta
  r.BindScratch()
  r.persisted = 0
  r.lastTokens = 0
  r.Loop.ElideOld(r.History)
  ui.Info("resumed %s (%s), %d messages", titleOr(r.Meta.Title), ShortID(r.Meta.ID), len(r.History))
}

func cmdTitle(r *REPL, _ context.Context, args []string) {
  if len(args) == 0 {
    ui.Info("title: %s", titleOr(r.Meta.Title))
    return
  }

  r.Meta.Title = strings.Join(args, " ")
  r.persisted = 0
  r.maybeAutosave()
  ui.Info("renamed to %s", r.Meta.Title)
}

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
    ui.Error("%s", err)
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
