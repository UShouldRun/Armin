package repl

import (
  "bufio"
  "context"
  "sync"
  "sync/atomic"
  "time"

  "armin/internal/agent"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/session"
  "armin/internal/share"
  "armin/internal/ui"
)

// flags packs REPL's boolean state into one byte instead of
// separate bool fields — mostly a style choice here (see
// internal/sandbox.RunFlags for a case where it also shrinks the
// struct); it reads as one coherent state word and scales better as
// more flags show up.
type flags uint8

// REPL owns the interactive session: history, the agent loop, and
// the slash-command surface layered on top of it.
type REPL struct {
  Loop    *agent.Loop
  LLM     *llm.Client

  // Completion serves ghost text (here and for a joined nvim);
  // Follower is it when it follows chat between local models.
  Completion, Follower *llm.Client
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

  // system is the system prompt as built, for Explain's one-off
  // questions (History[0] belongs to the REPL goroutine).
  system string

  // hub shares this REPL as the project's instance (see host.go):
  // other frontends' questions arrive through inbox and run here,
  // between prompts; busy rejects one while a turn runs.
  hub     *share.Hub
  inbox   chan func()
  wake    chan struct{}
  busy    atomic.Bool
  lines   atomic.Pointer[[]share.ChatLine]
  current    atomic.Pointer[string] // the chat model's name
  completing atomic.Pointer[string] // the completion model's name
  explain *agent.Loop            // read-only, for Explain
  runCtx  context.Context

  // pump owns stdin once Run starts: the prompt and every question
  // read keys through it.
  pump *ui.KeyPump

  // Terminal opens a new terminal window ("ghostty -e"); empty finds
  // one. proposal is what's waiting for approval, for [v]iew.
  Terminal string
  proposal *agent.Proposal

  // While a turn runs, the input bar (composer) takes messages:
  // queued until the turn's next step takes them; draft is what was
  // left unsent when it ended, for the next prompt.
  composer *ui.Composer
  queueMu  sync.Mutex
  queued   []string
  draft    string
}
