package lspserver

import (
  "context"
  "io"
  "sync"
  "sync/atomic"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/share"
)

// Server is a minimal, single-purpose LSP server: no diagnostics,
// no go-to-definition beyond ctags — hover, one ask command, and
// inline completion.
type Server struct {
  loop          *agent.Loop
  sandbox       *sandbox.Config
  system        string
  completionLLM *llm.Client
  follower      *llm.Client // completionLLM when it follows chat (see llamacpp.SwitchChat)
  models        *llamacpp.Manager
  symbols       *ctags.Index

  out   io.Writer
  outMu sync.Mutex

  docsMu  sync.Mutex
  docs    map[string]string  // uri -> full text, via didOpen/didChange
  edits   map[string]docEdit // uri -> where it was last edited
  editSeq uint64

  hoverMu    sync.Mutex
  hoverCache map[string]string

  inflightMu sync.Mutex
  inflight   map[string]inflightEntry // uri -> its running completion
  nextGen    int64

  // chat is armin.ask's conversation: kept across asks, as in the
  // REPL, until armin.clear. chatMu is held for a whole turn.
  chatMu sync.Mutex
  chat   []llm.Message

  // running maps an ask's request id to its cancel, for
  // $/cancelRequest (the chat window's <C-c>); chatCancel stops the
  // chat turn whoever started it.
  runningMu  sync.Mutex
  running    map[string]context.CancelFunc
  chatCancel context.CancelFunc

  // lines is the conversation as displayed, refreshed after each
  // turn: a joining armin gets it without waiting for a turn to end.
  lines atomic.Pointer[[]share.ChatLine]

  // hub shares this server's conversation and models when it is the
  // project's instance; remote is the instance it joined instead
  // (then loop, models and completionLLM are nil). origin names this
  // nvim's questions.
  hub    *share.Hub
  remote *share.Client
  origin string

  warnOnce sync.Once

  shutdownReceived bool
}

// docEdit is a document's last edited line, and when (a sequence
// number, not a clock) — input_extra context for completions.
type docEdit struct {
  line int
  seq  uint64
}

type inflightEntry struct {
  gen    int64
  cancel context.CancelFunc
}
