// Package share makes one armin per project: the first one started
// hosts the models and the conversation, and every armin started
// after it there — a terminal, nvim's -lsp — joins over a unix
// socket instead of loading models of its own. Questions from any of
// them show in all of them.
package share

import (
  "errors"
)

// ErrBusy: one turn at a time, whoever asked.
var ErrBusy = errors.New("still answering the previous question")

// EditingPrefix starts a question asked from a file; Lines strips it
// for display.
const EditingPrefix = "Currently editing: "
