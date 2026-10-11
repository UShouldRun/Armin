// Package repl is the interactive loop: reads a line, dispatches
// slash commands or forwards to the agent, streams the reply, and
// owns the session/context UX (plan Phase 4, plus the "way better
// TUI" and "/model, /context like Claude Code" asks on top of it).
package repl

import (
  "bufio"
  "context"
  "errors"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "sync"
  "time"

  "armin/internal/agent"
  "armin/internal/convo"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/session"
  "armin/internal/tools"
  "armin/internal/ui"
)

// compactKeepLast is how many recent messages /compact leaves
// untouched.
const compactKeepLast = 6

// suggestTimeout bounds the post-answer follow-up-suggestions call
// — it's a nice-to-have, not worth making the user wait long for.
const suggestTimeout = 20 * time.Second

// flags packs REPL's boolean state into one byte instead of
// separate bool fields — mostly a style choice here (see
// internal/sandbox.RunFlags for a case where it also shrinks the
// struct); it reads as one coherent state word and scales better as
// more flags show up.
type flags uint8

const (
  flagAutosave flags = 1 << iota
  flagQuit
  flagSuggest
  flagGhostText
  flagAutoApprove
  flagHideThinking
  flagAll = flagAutosave | flagQuit | flagSuggest | flagGhostText |
    flagAutoApprove | flagHideThinking
)

// REPL owns the interactive session: history, the agent loop, and
// the slash-command surface layered on top of it.
type REPL struct {
  Loop    *agent.Loop
  LLM     *llm.Client
  Models  *llamacpp.Manager
  Sandbox *sandbox.Config

  History   []llm.Message
  Root      string
  resumedAt *time.Time

  // Meta is this session's id, title and timestamps on disk.
  Meta session.Meta

  // ScratchBase is the project's scratch directory; each session
  // works in its own subdirectory of it (see BindScratch).
  ScratchBase string

  // persisted is how many leading messages of History are already
  // written to the session log — see internal/session.Append.
  // Mutating History in place (rather than only appending to it)
  // must reset this to 0, or the next autosave silently skips
  // messages that no longer match what's on disk.
  persisted int

  // lastSuggestions holds the follow-up questions shown after the
  // most recent answer (see cmdSuggest / maybeShowSuggestions), so
  // the next input can be a bare number picking one of them instead
  // of typing it out. Cleared as soon as anything else is sent.
  lastSuggestions []string

  // reader is stdin, shared by the prompt and approval questions.
  reader *bufio.Reader

  // cancelTurn cancels the turn in flight, if any — see Interrupt.
  turnMu     sync.Mutex
  cancelTurn context.CancelFunc

  // lastTokens is the server's count after the latest model call.
  lastTokens int

  // ContextFromServer re-reads the window from the server on a
  // /model switch (no context_size in config).
  ContextFromServer bool

  state flags
}

// New builds a REPL. If resumed is non-nil, its history is used
// as-is (skipping auto-context, since a resumed session already has
// it); otherwise a fresh system prompt + auto-context message seeds
// History.
func New(
  loop *agent.Loop, client *llm.Client, sb *sandbox.Config,
  systemPrompt string, resumed *session.Saved, autosave bool,
  models *llamacpp.Manager,
) *REPL {
  r := &REPL{
    Loop:    loop,
    LLM:     client,
    Models:  models,
    Sandbox: sb,
    Root:    sb.Root,
    reader:  bufio.NewReader(os.Stdin),
  }

  if autosave {
    r.state |= flagAutosave
  }

  if resumed != nil {
    r.History = resumed.History
    r.Meta = resumed.Meta
    r.Meta.Model = client.Model
    r.resumedAt = &resumed.UpdatedAt
    r.persisted = len(resumed.History) // already on disk
    // Today's prompt (memories, skills may have changed) replaces
    // the saved one, and old turns are trimmed like live ones.
    if len(r.History) > 0 && r.History[0].Role == "system" {
      r.History[0].Content = systemPrompt
      r.persisted = 0
    }

    loop.ElideOld(r.History)
    agent.SetModelLine(r.History, client.Model, models.Label(client.Model))

    return r
  }

  r.History = []llm.Message{ { Role: "system", Content: systemPrompt } }
  agent.SetModelLine(r.History, client.Model, models.Label(client.Model))
  r.Meta = session.Meta{ ID: session.NewID(), Root: sb.Root, Model: client.Model }

  return r
}

// LoadAutoContext silently orients the model with a directory
// listing and any README/manifest it can find, so the first answer
// doesn't require the user to explain the project by hand. Shares
// its logic with the re-invocable project_manifest tool (see
// tools.ProjectManifest) rather than duplicating it — this is just
// that same note, loaded once automatically at session start.
func (r *REPL) LoadAutoContext(ctx context.Context) {
  out, err := tools.ProjectManifest(ctx, r.Sandbox, tools.ProjectManifestInput{})
  if err != nil || out.Note == "" {
    return
  }

  // Merged into the system prompt, not a second system message:
  // Qwen-style templates reject a system message after the first.
  r.History[0].Content += "\n\n" + out.Note
}

// prompt is the input prompt with a context gauge in front: tokens
// used / window and the share, colored as compaction gets close. The
// server's count from the last request is exact; before the first
// one it's the rough estimate, marked "~".
func (r *REPL) prompt() string {
  used, mark := r.lastTokens, ""
  if used == 0 {
    used, mark = r.Loop.Estimate(r.History), "~"
  }

  window := r.Loop.WindowSize()
  if window <= 0 {
    return ui.Dim(mark + kTokens(used) + " ") + ui.Prompt()
  }

  pct := used * 100 / window
  gauge := fmt.Sprintf("%s%s/%s %d%%", mark, kTokens(used), kTokens(window), pct)
  if r.Loop.Tier() == "streaming" {
    gauge += " streaming"
  }

  switch {
  case pct >= agent.DefaultCompactAt:
    gauge = ui.Red(gauge)
  case pct >= 50:
    gauge = ui.Yellow(gauge)
  default:
    gauge = ui.Dim(gauge)
  }

  return gauge + " " + ui.Prompt()
}

// kTokens renders a token count compactly: 950, 8.4k, 96k.
func kTokens(n int) string {
  switch {
  case n < 1000:
    return fmt.Sprint(n)
  case n < 10000:
    return fmt.Sprintf("%.1fk", float64(n) / 1000)
  }

  return fmt.Sprintf("%dk", n / 1000)
}

// modelName is "alias — what serves it", or just the alias.
func (r *REPL) modelName() string {
  label := r.Models.Label(r.LLM.Model)
  if label == "" || label == r.LLM.Model {
    return r.LLM.Model
  }

  return r.LLM.Model + " — " + label
}

// SetAutoApprove skips the approval prompt for file writes.
func (r *REPL) SetAutoApprove(on bool) {
  if on {
    r.state |= flagAutoApprove
  } else {
    r.state &^= flagAutoApprove
  }
}

// Interrupt cancels the turn in flight and reports whether there
// was one — so Ctrl-C stops the model, not armin.
func (r *REPL) Interrupt() bool {
  r.turnMu.Lock()
  defer r.turnMu.Unlock()
  if r.cancelTurn == nil {
    return false
  }

  r.cancelTurn()
  r.cancelTurn = nil

  return true
}

// Run drives the interactive loop until /quit, EOF, or an input
// error.
func (r *REPL) Run(ctx context.Context) error {
  ui.Banner(r.Root, r.modelName(), r.LLM.BaseURL)
  if w := r.Loop.Window; w != nil {
    ui.Info(
      "  context: %dk in VRAM, grows to %dk with KV streaming when a task needs it",
      w.Resident() / 1000, w.Max() / 1000,
    )
  }

  switch {
  case r.resumedAt != nil:
    ui.Info(
      "  session: %s (%s), last saved %s",
      titleOr(r.Meta.Title), ShortID(r.Meta.ID), r.resumedAt.Format("Jan 2 15:04"),
    )
  default:
    ui.Info("  session: new (%s)", ShortID(r.Meta.ID))
  }

  for r.state & flagQuit == 0 {
    input, err := ui.ReadLineWithGhostText(
      ctx, r.reader, "\n" + r.prompt(), r.suggestFunc(),
    )
    if errors.Is(err, ui.ErrInterrupted) {
      fmt.Println()
      continue // Ctrl-C on a partial line: fresh prompt, don't quit
    }

    if errors.Is(err, io.EOF) {
      break
    }

    if err != nil {
      return fmt.Errorf("\n\t... in Run:\n\t input error: %w", err)
    }

    input = strings.TrimSpace(input)
    if input == "" {
      continue
    }

    if picked, ok := r.pickSuggestion(input); ok {
      input = picked
    } else {
      r.lastSuggestions = nil
    }

    if strings.HasPrefix(input, "/") {
      r.dispatch(ctx, input)
      continue
    }

    if err := r.turn(ctx, input); err != nil {
      if errors.Is(err, llm.ErrToolsUnsupported) {
        ui.Error(
          "%q doesn't support tool calling, so it can't drive"+
            " armin's tools. Switch to one that does with"+
            " /model <name> (/model with no name lists what's"+
            " configured).", r.LLM.Model,
        )
      } else {
        ui.Error("%s", err)
      }
    }

    r.maybeTitle(ctx)
    r.maybeAutosave()
  }

  return nil
}

// suggestFunc returns the ghost-text callback ReadLineWithGhostText
// should use, or nil when ghost text is off — nil is what tells
// ReadLineWithGhostText to skip raw mode entirely and fall back to
// a plain read, so turning this off costs nothing.
func (r *REPL) suggestFunc() ui.SuggestFunc {
  if r.state & flagGhostText == 0 {
    return nil
  }

  return r.suggestContinuation
}

// pickSuggestion checks whether input is a bare number selecting
// one of the follow-ups shown after the last answer.
func (r *REPL) pickSuggestion(input string) (string, bool) {
  if len(r.lastSuggestions) == 0 {
    return "", false
  }

  n, err := strconv.Atoi(input)
  if err != nil || n < 1 || n > len(r.lastSuggestions) {
    return "", false
  }

  return r.lastSuggestions[n - 1], true
}

// maybeShowSuggestions prints 2-3 numbered follow-up questions after
// an answer, if /suggest is on — pick one next by typing its number
// instead of writing out the question.
func (r *REPL) maybeShowSuggestions(ctx context.Context) {
  if r.state & flagSuggest == 0 {
    return
  }

  suggestCtx, cancel := context.WithTimeout(ctx, suggestTimeout)
  defer cancel()

  suggestions, err := convo.Suggest(suggestCtx, r.LLM, r.History)
  if err != nil || len(suggestions) == 0 {
    return // a nice-to-have; fail silently rather than interrupt
  }

  r.lastSuggestions = suggestions
  fmt.Println()
  for i, s := range suggestions {
    fmt.Println(ui.Dim(fmt.Sprintf("  [%d] %s", i + 1, s)))
  }
}

// suggestContinuation implements ui.SuggestFunc for the REPL's
// ghost-text-while-typing: a fast, non-agentic raw completion (see
// llm.Client.Complete) continuing the partial message, not a chat
// reply to it.
func (r *REPL) suggestContinuation(ctx context.Context, partial string) string {
  if strings.TrimSpace(partial) == "" {
    return ""
  }

  out, err := r.LLM.Complete(ctx, partial, "", llm.CompleteOptions{
    Temperature: 0.3,
    NumPredict:  24,
    Stop:        []string{ "\n" },
  })
  if err != nil {
    return ""
  }

  return strings.TrimRight(out, " \t")
}

// maybeAutosave appends only the messages added since the last save
// — see internal/session.Append — so autosaving after every turn
// stays O(1) amortized instead of re-writing the whole, ever-growing
// history each time.
func (r *REPL) maybeAutosave() {
  if r.state & flagAutosave == 0 {
    return
  }

  r.Meta.Model = r.LLM.Model
  err := session.Append(&r.Meta, r.History, &r.persisted)
  if err != nil {
    ui.Error("failed to save session: %s", err)
  }
}

// SaveNow writes the whole session (the exit flush), unless saving
// is off or nothing was ever said.
func (r *REPL) SaveNow() error {
  if r.state & flagAutosave == 0 || !r.hasTurns() {
    return nil
  }

  r.Meta.Model = r.LLM.Model

  return session.Save(&r.Meta, r.History)
}

// hasTurns reports whether the user said anything yet: an empty
// session isn't worth a file.
func (r *REPL) hasTurns() bool {
  for _, m := range r.History {
    if m.Role == "user" {
      return true
    }
  }

  return false
}

// titleTimeout bounds naming the session.
const titleTimeout = 30 * time.Second

// maybeTitle has the model name the session after its first turn,
// once; the name is what the resume menu shows.
func (r *REPL) maybeTitle(ctx context.Context) {
  if r.Meta.Title != "" || r.state & flagAutosave == 0 || !r.hasTurns() {
    return
  }

  ctx, cancel := context.WithTimeout(ctx, titleTimeout)
  defer cancel()

  spinner := ui.NewSpinner("naming this session")
  r.Meta.Title = convo.Title(ctx, r.LLM, r.History)
  spinner.Stop()

  r.persisted = 0 // the title lives in the first line: rewrite once
  ui.Info("session: %s (%s)", r.Meta.Title, ShortID(r.Meta.ID))
}

func titleOr(t string) string {
  if t == "" {
    return "untitled"
  }

  return t
}

// BindScratch points the scratchpad at this session's own folder,
// so two sessions of one project don't share plans, archives or
// thinking notes. Memory stays per project: durable facts should
// carry across sessions. A session from before ids existed keeps the
// project-wide folder it already used.
func (r *REPL) BindScratch() {
  if r.Loop.Scratch == nil || r.ScratchBase == "" {
    return
  }

  r.Loop.Scratch.Dir = r.ScratchBase
  if isUUID(r.Meta.ID) {
    r.Loop.Scratch.Dir = filepath.Join(r.ScratchBase, r.Meta.ID)
  }
}

func isUUID(id string) bool {
  return len(id) == 36 && strings.Count(id, "-") == 4
}

// ShortID is the first block of a UUID: enough to resume by.
func ShortID(id string) string {
  if i := strings.IndexByte(id, '-'); i > 0 {
    return id[:i]
  }

  return id
}
