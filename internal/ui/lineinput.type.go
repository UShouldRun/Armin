// The types of lineinput.go.

package ui

import (
  "bufio"
  "context"
  "sync"
)

// SuggestFunc computes a ghost-text continuation for the given
// partial input, or "" for none. Called from a background goroutine
// after a typing pause; ctx is canceled if the input changes again
// before it returns, or the line is submitted/abandoned.
type SuggestFunc func(ctx context.Context, partial string) string

// lineState is the pure, terminal-independent line-editing state: a
// buffer plus the ghost-text suggestion currently shown for it.
// Kept separate from the raw terminal I/O below so it can be unit
// tested without a real TTY.
//
// A large paste sits in buf as one rune, pasteBase + its index in
// pastes: shown as a short label, sent as the text. Editing only
// happens at the end, so the pastes buf holds are always a prefix
// of pastes.
type lineState struct {
  buf        []rune
  suggestion []rune
  pastes     [][]rune
}

type outcome struct {
  submit, interrupted, eof, changed bool
}

// KeyPump decodes keys from a reader on its own goroutine, so a read
// can give way to output (see ReadLineWake) without losing a key.
type KeyPump struct {
  reader *bufio.Reader
  keys   chan keyRead
  once   sync.Once
}

type keyRead struct {
  k   key
  err error
}
