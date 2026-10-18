// Package share makes one armin per project: the first one started
// hosts the models and the conversation, and every armin started
// after it there — a terminal, nvim's -lsp — joins over a unix
// socket instead of loading models of its own. Questions from any of
// them show in all of them.
//
// The wire is one JSON object per line: a joined armin sends
// Requests (ask, cancel, clear, model, complete, ...), the host sends
// Events (tokens, tool calls, replies) to everyone joined.
//
//   socket.go    where a project's socket lives (a hash of its root,
//                in a directory only the user can open)
//   hub.go       the host's side: Listen, Emit, one goroutine per
//                joined armin answering its Requests through Host
//   client.go    a joined armin's side: Dial, then Ask, Stop,
//                Complete, ... as calls over the socket
//   lines.go     a history as chat lines; questions asked from a file
//   protocol.go  this overview, the errors
//
// Host (protocol.type.go) is what a hosting frontend implements: the
// REPL (repl/host.go) and nvim's LSP server do.
package share

import (
  "errors"
)

// ErrBusy: one turn at a time, whoever asked.
var ErrBusy = errors.New("still answering the previous question")

// EditingPrefix starts a question asked from a file; Lines strips it
// for display.
const EditingPrefix = "Currently editing: "
