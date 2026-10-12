package share

import (
  "context"
  "encoding/json"

  "armin/internal/catalog"
  "armin/internal/llm"
)

// Event is one thing that happened in the shared conversation, or a
// reply to a Request. To nvim it goes as armin/askEvent.
//
// Type: "user" (a question, Delta), "token", "notice", "tool_result"
// (Delta), "reasoning", "tool_call" (Name/Args), "usage", "done"
// (the answer), "error", "cleared", "model" (Model: after a switch);
// to a joined armin also "hello" (Delta: its origin, Instance,
// Model), "history" and "reply" (Reply: the Request's Seq).
type Event struct {
  ID       json.RawMessage `json:"id,omitempty"` // nvim's request id
  Type     string          `json:"type"`
  Origin   string          `json:"origin,omitempty"` // who asked: "nvim", "terminal", "join:N"
  Name     string          `json:"name,omitempty"`
  Args     map[string]any  `json:"args,omitempty"`
  Delta    string          `json:"delta,omitempty"`
  Error    string          `json:"error,omitempty"` // "reply": what failed
  Tier     string          `json:"tier,omitempty"`
  Tokens   int             `json:"tokens,omitempty"`  // context used ("usage")
  Window   int             `json:"window,omitempty"`  // context window ("usage")
  Reply    uint32          `json:"reply,omitempty"`   // "reply"
  Instance int             `json:"instance,omitempty"`
  History  []ChatLine      `json:"history,omitempty"`
  Model    *ModelInfo      `json:"model,omitempty"`
}

// ChatLine is one displayed line of the conversation: a question,
// an answer, or a tool call.
type ChatLine struct {
  Role string `json:"role"` // "user", "assistant", "tool"
  Text string `json:"text"`
}

// ModelChoice is one configured model.
type ModelChoice struct {
  Name    string `json:"name"`
  Model   string `json:"model,omitempty"` // what serves it: the .gguf name, a machine, a URL
  Managed bool   `json:"managed"`         // false: armin only talks to it
  Running bool   `json:"running"`         // a managed process is up for it

  // Cost is a hosted model's price, when the catalog knows it.
  Cost *catalog.Suggestion `json:"cost,omitempty"`
}

// ModelInfo is the models in use, every configured one, and which
// instance this is.
type ModelInfo struct {
  Current    string        `json:"current"`
  Completion string        `json:"completion,omitempty"` // serves inline completion
  Instance   int           `json:"instance,omitempty"`
  Configured []ModelChoice `json:"configured,omitempty"`

  // Hosted is the configured OpenRouter model whose endpoint the
  // Suggested ones reuse, switched to as "<Hosted>:<id>".
  Hosted    string               `json:"hosted,omitempty"`
  Suggested []catalog.Suggestion `json:"suggested,omitempty"`
  Balance   *catalog.Balance     `json:"balance,omitempty"`
}

// Request is what a joined armin sends, one JSON per line. Op:
// "ask" (Text, File), "cancel", "clear", "history", "model" (Text:
// a name to switch to, or empty to list), "complete" (Fill),
// "explain" (Text), "drop" (Seq: stop that complete or explain).
// Seq, when set, gets a "reply" event.
type Request struct {
  Op   string `json:"op"`
  Seq  uint32 `json:"seq,omitempty"`
  Text string `json:"text,omitempty"`
  File string `json:"file,omitempty"` // ask: the file being edited
  Fill *Fill  `json:"fill,omitempty"`
}

// Fill is one inline completion, cut out by the frontend that has
// the buffer (see lspserver.fimWindow).
type Fill struct {
  Key    string           `json:"key"` // a newer Fill with the same Key supersedes this one
  Prefix string           `json:"prefix"`
  Suffix string           `json:"suffix"`
  Opts   llm.CompleteOptions `json:"opts"`
}

// Host is the armin that thinks: it owns the models and the
// conversation. lspserver.Server and repl.REPL are hosts.
type Host interface {
  // Ask starts a turn of the shared conversation for origin; its
  // events go to every frontend. ErrBusy while one runs.
  Ask(ctx context.Context, question, file, origin string) error
  Stop()
  Clear(origin string) error
  History() []ChatLine
  Models() ModelInfo
  SwitchModel(ctx context.Context, name string) (ModelInfo, error)

  // Complete and Explain stand apart from the conversation: inline
  // completion, and hover's one-off question.
  Complete(ctx context.Context, f Fill) (string, error)
  Explain(ctx context.Context, question string) (string, error)
}
