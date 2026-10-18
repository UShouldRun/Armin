// Keeping a long session inside the model's window. After each
// model call manageContext asks Decide (policy.go) what the next
// request needs: trimming old tool results to stubs (archived to the
// scratchpad first), saving notes and compacting older history into a
// summary, or moving to the larger streamed window. endTurn and
// ElideOld shrink finished turns; hintCompact offers the user a
// compaction before it becomes urgent.

package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "strings"
  "time"

  "armin/internal/convo"
  "armin/internal/llm"
)

// DefaultKeepTurns is how many recent user turns keep their full
// tool results.
const DefaultKeepTurns = 2

const (
  // elideMinBytes: smaller tool results aren't worth a stub.
  elideMinBytes = 600
  // elideArgBytes: write_file/edit_file arguments over this are
  // shrunk in aged turns — the file on disk has them now.
  elideArgBytes = 400
  // compactKeepLast is how many recent messages auto-compaction
  // keeps verbatim.
  compactKeepLast = 8
  // argPreview caps each argument value in a stub.
  argPreview = 80
  // saveRounds caps the save-before-compact exchange.
  saveRounds = 3
  // thinkingFile is the scratch file finished turns' reasoning goes to.
  thinkingFile = "thinking"
)

const elidedPrefix = `{"elided":`

// pinned tools keep their results through a soft trim: a loaded
// skill or memory is small and stays relevant for the whole task.
var pinned = map[ToolID]bool{ toolUseSkill: true, toolMemoryRead: true }

func (l *Loop) keepTurns() int {
  if l.KeepTurns == 0 {
    return DefaultKeepTurns
  }

  return l.KeepTurns
}

// window is the active context window: the tier control's when
// there is one, else the fixed ContextSize.
func (l *Loop) window() int {
  if l.Window != nil {
    return l.Window.Size()
  }

  return l.ContextSize
}

// modelLinePrefix starts the system prompt's line naming the model.
const modelLinePrefix = "You are running as model "

// WithModelLine returns prompt with its model line set to name and
// label (what serves it), replacing an earlier one.
func WithModelLine(prompt, name, label string) string {
  line := modelLinePrefix + "`" + name + "`"
  if label != "" && label != name {
    line += " (" + label + ")"
  }

  line += "."

  lines := strings.Split(prompt, "\n")
  for i, l := range lines {
    if strings.HasPrefix(l, modelLinePrefix) {
      lines[i] = line
      return strings.Join(lines, "\n")
    }
  }

  return prompt + "\n\n" + line
}

// SetModelLine updates the model line in history's system prompt —
// after a /model switch. The switch reloads the server anyway, so
// the prompt-cache cost of editing the first message is nil.
func SetModelLine(history []llm.Message, name, label string) {
  if len(history) > 0 && history[0].Role == "system" {
    history[0].Content = WithModelLine(history[0].Content, name, label)
  }
}

// WindowSize is the active context window in tokens (0: unknown).
func (l *Loop) WindowSize() int { return l.window() }

// Tier names the active context tier ("" without a stream tier).
func (l *Loop) Tier() string {
  switch {
  case l.Window == nil:
    return ""
  case l.Window.Streaming():
    return "streaming"
  }

  return "resident"
}

// endTurn runs once per turn, success or not. It moves the turn's
// reasoning to the scratchpad's thinking file and drops it from
// history — chat templates stop rendering it once the next user
// message arrives anyway, so this changes no prompt tokens and costs
// no KV-cache reuse, while the model can still re-read it.
func (l *Loop) endTurn(history *[]llm.Message, turnStart int, _ Hooks) {
  h := *history
  from := max(0, turnStart - 1)

  var thoughts strings.Builder
  for i := from; i < len(h); i++ {
    if h[i].Reasoning != "" {
      thoughts.WriteString(strings.TrimSpace(h[i].Reasoning))
      thoughts.WriteString("\n\n")
    }

    h[i].Reasoning = ""
  }

  if l.Scratch == nil || thoughts.Len() == 0 {
    return
  }

  label := time.Now().Format("2006-01-02 15:04") + " — " + turnTopic(h, from)
  l.Scratch.Write(thinkingFile, "## " + label + "\n" + thoughts.String(), true)
}

// turnTopic is the turn's user message, cut short, for labels.
func turnTopic(h []llm.Message, from int) string {
  for i := from; i < len(h); i++ {
    if h[i].Role == "user" {
      topic := strings.Join(strings.Fields(h[i].Content), " ")
      if len(topic) > 60 {
        topic = topic[:60] + "…"
      }

      return topic
    }
  }

  return "turn"
}

// ElideOld trims every turn older than KeepTurns — for a resumed
// session, whose saved log holds full results. O(history), once.
func (l *Loop) ElideOld(history []llm.Message) {
  keep := l.keepTurns()
  if keep < 0 {
    return
  }

  from := keptFrom(history, keep)
  if from > 0 {
    l.elide(history[:from], true)
  }
}

// elide replaces big tool results with a stub naming the call that
// produced them, and shrinks big write arguments. With a scratchpad,
// the full result is archived first and the stub points at it: an
// exact re-read beats re-running a slow or non-deterministic tool
// (tests, web search). soft keeps pinned tools' results.
func (l *Loop) elide(msgs []llm.Message, soft bool) (saved int) {
  calls := map[string]llm.ToolCall{}

  for i := range msgs {
    m := &msgs[i]
    switch m.Role {
    case "assistant":
      for _, c := range m.ToolCalls {
        calls[c.ID] = c
        saved += shrinkArgs(c)
      }

    case "tool":
      if len(m.Content) < elideMinBytes ||
        strings.HasPrefix(m.Content, elidedPrefix) {
        continue
      }

      if soft && pinned[toolIDs[m.ToolName]] {
        continue
      }

      what := describeCall(m.ToolName, calls[m.ToolCallID])
      stub := map[string]any{ "elided": what, "bytes": len(m.Content) }
      if l.Scratch != nil {
        name, start, end, err := l.Scratch.Archive(what, m.Content)
        if err == nil {
          stub["archived"] = fmt.Sprintf(
            "scratchpad_read name=%s start_line=%d end_line=%d",
            name, start, end,
          )
        }
      }

      if stub["archived"] == nil {
        stub["note"] = "old result dropped to save context; " +
          "call again if needed"
      }

      raw, _ := json.Marshal(stub)
      saved += len(m.Content) - len(raw)
      m.Content = string(raw)
    }
  }

  return saved
}

func shrinkArgs(c llm.ToolCall) (saved int) {
  if !toolIDs[c.Function.Name].isWrite() {
    return 0
  }

  for _, key := range []string{ "content", "old_string", "new_string" } {
    v, ok := c.Function.Arguments[key].(string)
    if !ok || len(v) <= elideArgBytes {
      continue
    }

    note := fmt.Sprintf("[%d bytes, elided — written to disk]", len(v))
    c.Function.Arguments[key] = note
    saved += len(v) - len(note)
  }

  return saved
}

// describeCall renders "read_file path=x start_line=10" with long
// values cut short.
func describeCall(name string, c llm.ToolCall) string {
  var b strings.Builder
  b.WriteString(name)
  for k, v := range c.Function.Arguments {
    s := fmt.Sprint(v)
    if len(s) > argPreview {
      s = s[:argPreview] + "…"
    }

    fmt.Fprintf(&b, " %s=%s", k, s)
  }

  return b.String()
}

// estimate is the context the next request will occupy: the
// server's own count for the last reply, plus a rough count of what
// was appended since (tool results). Without a server count it
// falls back to the rough count for everything.
func (l *Loop) estimate(h []llm.Message, last llm.Usage) int {
  if last.Total() == 0 {
    return l.Estimate(h)
  }

  extra := 0
  for i := len(h) - 1; i >= 0 && h[i].Role != "assistant"; i-- {
    extra += convo.ApproxTokens(h[i])
  }

  return last.Total() + extra
}

// keptFrom is the index of the keep-th most recent user message:
// everything before it is old enough to trim.
func keptFrom(h []llm.Message, keep int) int {
  users := 0
  for i := len(h) - 1; i >= 0; i-- {
    if h[i].Role != "user" {
      continue
    }

    users++
    if users == keep {
      return i
    }
  }

  return 0
}

// state builds what Decide looks at; the working set is everything
// from the latest user message on.
func (l *Loop) state(h []llm.Message, used int) ContextState {
  s := ContextState{ Used: used, Window: l.window() }
  if l.Window != nil {
    s.Resident, s.Max = l.Window.Resident(), l.Window.Max()
    s.Streaming = l.Window.Streaming()
  } else {
    s.Resident, s.Max = s.Window, s.Window
  }

  s.Turn = convo.Analyze(h[keptFrom(h, 1):]).ApproxTokens
  s.Fixed = l.defsTokens
  if len(h) > 0 && h[0].Role == "system" {
    s.Fixed += convo.ApproxTokens(h[0])
  }

  return s
}

func notice(hooks Hooks, format string, args ...any) {
  if hooks.Notice != nil {
    hooks.Notice(fmt.Sprintf(format, args...))
  }
}

// manageContext runs Decide and carries it out before a model call.
// Reports whether history was rewritten (or the server restarted),
// so the caller drops its server-side token count.
func (l *Loop) manageContext(
  ctx context.Context, history *[]llm.Message,
  last llm.Usage, hooks Hooks,
) bool {
  used := l.estimate(*history, last)
  act := Decide(l.state(*history, used))
  if act == 0 {
    return false
  }

  h := *history
  before := convo.Analyze(h).ApproxTokens

  // Trim first: it's free, and it may be all that's needed.
  keep := l.keepTurns()
  if keep > 0 {
    saved := l.elide(h[:keptFrom(h, keep)], true)
    if saved > 0 {
      where := "dropped"
      if l.Scratch != nil {
        where = "archived to the scratchpad"
      }

      notice(hooks,
        "context ~%dk of %dk — trimmed old tool results (~%dk tokens %s)",
        used / 1000, l.window() / 1000, saved / 4000, where,
      )
    }
  }

  after := convo.Analyze(h).ApproxTokens
  if after <= l.window() * DefaultCompactAt / 100 {
    act &^= ActGrow | ActSave | ActCompact
  }

  if act & ActGrow != 0 {
    notice(hooks,
      "context ~%dk of %dk is mostly this turn's work — growing the "+
        "window to %dk with KV streaming (one reload + re-prefill)",
      after / 1000, l.window() / 1000, l.Window.Max() / 1000,
    )
    _, err := l.Window.Grow(ctx)
    if err != nil {
      notice(hooks, "couldn't grow the window (%s); compacting instead", err)
      act |= ActSave | ActCompact
    }
  }

  if act & ActSave != 0 {
    l.saveBeforeCompact(ctx, *history, hooks)
  }

  if act & ActCompact != 0 {
    l.compact(ctx, history, hooks)
    after = convo.Analyze(*history).ApproxTokens
    if DecideAfterCompact(l.state(*history, after)) & ActShrink != 0 {
      notice(hooks,
        "context is down to ~%dk — back to the fast %dk window",
        after / 1000, l.Window.Resident() / 1000,
      )
      _, err := l.Window.Shrink(ctx)
      if err != nil {
        notice(hooks, "couldn't shrink the window: %s", err)
      }
    }
  }

  after = convo.Analyze(*history).ApproxTokens
  if hooks.Compacted != nil && after < before {
    hooks.Compacted(before, after)
  }

  return true
}

// compactHint is the note behind SuggestCompact. It goes in as its
// own "[armin]" message after the user's, at the end of history, so
// the cached prefix stays intact and frontends keep it out of view.
const compactHint = compactHintPrefix + "~%d%% full (~%dk of %dk " +
  "tokens). At %d%% armin compacts it automatically, which may land " +
  "in the middle of a task. Answer the user's message first. Then, " +
  "only if the work is at a natural break (a task finished, or a new " +
  "topic starting), end your reply by asking whether to compact now " +
  "with /compact (the compact button in the web page). Before asking, " +
  "make sure your plan, progress and key findings are in the " +
  "scratchpad, and tell them that after compacting you'll re-read the " +
  "files and notes you still need rather than work from memory. If " +
  "you're in the middle of a task, don't ask. This note isn't repeated."

const compactHintPrefix = "[armin] Context is "

// hintCompact adds compactHint at the start of a turn, once per
// stretch of history: a compaction summary starts a new one.
func (l *Loop) hintCompact(history *[]llm.Message, hooks Hooks) {
  h := *history
  if len(h) == 0 || h[len(h) - 1].Role != "user" {
    return
  }

  used := l.Estimate(h)
  if !SuggestCompact(l.state(h, used)) {
    return
  }

  for i := len(h) - 1; i > 0; i-- {
    if h[i].Role != "user" {
      continue
    }

    if convo.IsSummary(h[i]) {
      break
    }

    if strings.HasPrefix(h[i].Content, compactHintPrefix) {
      return
    }
  }

  window := l.window()
  notice(hooks,
    "context ~%dk of %dk — the model will offer to compact at a break",
    used / 1000, window / 1000,
  )
  *history = append(h, llm.Message{
    Role: "user",
    Content: fmt.Sprintf(compactHint,
      used * 100 / window, used / 1000, window / 1000, DefaultCompactAt,
    ),
  })
}

// saveNote asks the model to persist what it needs before older
// history is summarized away.
const saveNote = "Your context window is nearly full: older messages " +
  "are about to be summarized. Before that, save what you will " +
  "still need. Write your plan, progress and key findings (file " +
  "paths, line numbers, decisions, open questions) to the scratchpad " +
  "with scratchpad_write. Save durable facts about the user or the " +
  "project with memory_save. Then reply with one line: saved."

// saveBeforeCompact gives the model a short, tool-limited turn to
// choose what goes to the scratchpad and what to memory. It runs on
// a copy: the files it writes are the point, and the exchange itself
// must not become "the user's latest request" for the summary.
func (l *Loop) saveBeforeCompact(
  ctx context.Context, history []llm.Message, hooks Hooks,
) {
  var defs []llm.ToolDefinition
  for _, d := range l.Definitions() {
    switch toolIDs[d.Function.Name] {
    case toolScratchpadWrite, toolScratchpadRead, toolMemorySave:
      defs = append(defs, d)
    }
  }

  if len(defs) == 0 {
    return
  }

  notice(hooks, "context nearly full — letting the model save "+
    "notes before compacting")
  ask := make([]llm.Message, len(history), len(history) + 1 + saveRounds * 2)
  copy(ask, history)
  ask = append(ask, llm.Message{ Role: "user", Content: saveNote })

  client := l.LLM.Chore()
  for round := 0; round < saveRounds; round++ {
    reply, err := client.Chat(ctx, ask, defs, nil)
    if err != nil {
      return
    }

    reply.Reasoning = ""
    ask = append(ask, *reply)
    if len(reply.ToolCalls) == 0 {
      return
    }

    for _, call := range reply.ToolCalls {
      if hooks.ToolCall != nil {
        hooks.ToolCall(call.Function.Name, call.Function.Arguments)
      }

      raw, _ := json.Marshal(l.execute(ctx, call, hooks))
      if hooks.ToolResult != nil {
        hooks.ToolResult(call.Function.Name, string(raw))
      }

      ask = append(ask, llm.Message{
        Role:       "tool",
        ToolName:   call.Function.Name,
        ToolCallID: call.ID,
        Content:    string(raw),
      })
    }
  }
}

// compact stubs everything before the current turn, then has the
// model summarize older history if that wasn't enough.
func (l *Loop) compact(
  ctx context.Context, history *[]llm.Message, hooks Hooks,
) {
  h := *history
  l.elide(h[:keptFrom(h, 1)], false)
  enough := l.window() * DefaultCompactAt / 100 * 2 / 3
  if convo.Analyze(h).ApproxTokens <= enough {
    return
  }

  extra := ""
  if l.Scratch != nil {
    s := l.Scratch.Summary()
    if s != "" {
      extra = "Scratchpad files (scratchpad_read to open):\n" + s
    }
  }

  notice(hooks,
    "summarizing ~%dk tokens of older history",
    convo.Analyze(h).ApproxTokens / 1000,
  )

  // A failed summary isn't fatal: the request may still fit, and if
  // not the server's own error says so.
  convo.Compact(ctx, l.LLM, history, compactKeepLast, extra)
}
