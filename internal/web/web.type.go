// The server's config and state, and the API's request and reply
// shapes.

package web

import (
  "net/http"
  "sync"
  "sync/atomic"

  "armin/internal/agent"
  "armin/internal/catalog"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/session"
)

// Config is what Serve needs from cmd/armin.
type Config struct {
  Addr         string
  Loop         *agent.Loop
  Client       *llm.Client
  Models       *llamacpp.Manager
  Sandbox      *sandbox.Config
  SystemPrompt string
  // AutoApprove applies file writes without asking.
  AutoApprove bool

  // Resumed, if set, is the session to continue (armin -resume).
  Resumed *session.Saved
  // Autosave writes the session after every turn, like the REPL.
  Autosave bool
  // ScratchBase is the project's scratch folder; the session's own
  // scratchpad is a folder under it named by the session id.
  ScratchBase string
  // Effort is how hard the model thinks; a request may override it.
  Effort agent.Effort
}

// Server holds the one browser session.
type Server struct {
  cfg Config

  mu        sync.Mutex // guards history and turns: one turn at a time
  history   []llm.Message
  tokens    int
  meta      session.Meta
  persisted int // messages of history already on disk

  approvals sync.Map // id → chan decision
  nextID    atomic.Uint64
}

type decision struct {
  OK       bool   `json:"ok"`
  Feedback string `json:"feedback"`
}

// modelChoice is one entry of the page's model picker.
type modelChoice struct {
  Name  string              `json:"name"`
  Model string              `json:"model"`
  Cost  *catalog.Suggestion `json:"cost,omitempty"` // a hosted model's price
}

type infoBody struct {
  Root      string        `json:"root"`
  Model     string        `json:"model"`
  Label     string        `json:"label"`
  Models    []modelChoice `json:"models"`
  Window    int      `json:"window"`
  Tokens    int      `json:"tokens"`
  Approx    int      `json:"approx"`
  Tier      string   `json:"tier"`
  Messages  int      `json:"messages"`
  AutoApply bool     `json:"auto_apply"`

  // Warning says why local models will be slow (llamacpp.GPUWarning).
  Warning string `json:"warning,omitempty"`

  // Hosted is the OpenRouter model Suggested ones reuse, as
  // "<Hosted>:<id>"; Balance is what the account has left.
  Hosted    string               `json:"hosted,omitempty"`
  Suggested []catalog.Suggestion `json:"suggested,omitempty"`
  Balance   *catalog.Balance     `json:"balance,omitempty"`
}

// sse writes one server-sent event and flushes it.
type sse struct {
  w  http.ResponseWriter
  fl http.Flusher
  mu sync.Mutex
}

// historyEntry is one line of the conversation for the page to
// redraw a resumed session.
type historyEntry struct {
  Role string `json:"role"` // user, assistant, tool
  Text string `json:"text"`
}
