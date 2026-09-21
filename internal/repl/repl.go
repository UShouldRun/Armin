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
  "strconv"
  "strings"
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

// turnTimeout bounds one user turn end to end (all tool rounds
// included), so a hung model call can't wedge the REPL forever.
const turnTimeout = 5 * time.Minute

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
  flagAll = flagAutosave | flagQuit | flagSuggest | flagGhostText
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
  }
  if autosave {
    r.state |= flagAutosave
  }

  if resumed != nil {
    r.History = resumed.History
    r.resumedAt = &resumed.UpdatedAt
    r.persisted = len(resumed.History) // already on disk
    return r
  }

  r.History = []llm.Message{ { Role: "system", Content: systemPrompt } }
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
  r.History = append(
    r.History, llm.Message{ Role: "system", Content: out.Note },
  )
}

// Run drives the interactive loop until /quit, EOF, or an input
// error.
func (r *REPL) Run(ctx context.Context) error {
  ui.Banner(r.Root, r.LLM.Model, r.LLM.BaseURL)
  if r.resumedAt != nil {
    ui.Info("resumed session from %s", r.resumedAt.Format(time.RFC822))
  }

  reader := bufio.NewReader(os.Stdin)

  for r.state&flagQuit == 0 {
    input, err := ui.ReadLineWithGhostText(
      ctx, reader, "\n"+ui.Prompt(), r.suggestFunc(),
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
    r.maybeAutosave()
  }

  return nil
}

// suggestFunc returns the ghost-text callback ReadLineWithGhostText
// should use, or nil when ghost text is off — nil is what tells
// ReadLineWithGhostText to skip raw mode entirely and fall back to
// a plain read, so turning this off costs nothing.
func (r *REPL) suggestFunc() ui.SuggestFunc {
  if r.state&flagGhostText == 0 {
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
  return r.lastSuggestions[n-1], true
}

// turn runs one user message through the agent loop, streaming the
// reply and showing tool calls live as they happen.
func (r *REPL) turn(ctx context.Context, input string) error {
  r.lastSuggestions = nil
  r.History = append(
    r.History, llm.Message{ Role: "user", Content: input },
  )

  spinner := ui.NewSpinner("thinking")
  firstToken := true

  onToken := func(delta string) {
    if firstToken {
      spinner.Stop()
      spinner = nil
      firstToken = false
    }
    fmt.Print(delta)
  }

  onToolCall := func(name string, args map[string]any) {
    if !firstToken {
      fmt.Println()
    }
    if spinner != nil {
      spinner.Stop()
    }
    ui.ToolCall(name, args)
    spinner = ui.NewSpinner("thinking")
    firstToken = true
  }

  turnCtx, cancel := context.WithTimeout(ctx, turnTimeout)
  defer cancel()

  answer, err := r.Loop.Turn(turnCtx, &r.History, onToken, onToolCall)

  if spinner != nil {
    spinner.Stop()
  }
  if err != nil {
    return err
  }

  if !firstToken {
    fmt.Println()
  } else if answer != "" {
    fmt.Println(answer)
  }

  r.maybeShowSuggestions(ctx)
  return nil
}

// maybeShowSuggestions prints 2-3 numbered follow-up questions after
// an answer, if /suggest is on — pick one next by typing its number
// instead of writing out the question.
func (r *REPL) maybeShowSuggestions(ctx context.Context) {
  if r.state&flagSuggest == 0 {
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
    fmt.Println(ui.Dim(fmt.Sprintf("  [%d] %s", i+1, s)))
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
  if r.state&flagAutosave == 0 {
    return
  }
  err := session.Append(r.Root, r.LLM.Model, r.History, &r.persisted)
  if err != nil {
    ui.Error("failed to save session: %s", err)
  }
}
