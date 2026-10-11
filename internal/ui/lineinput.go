package ui

import (
  "bufio"
  "context"
  "errors"
  "fmt"
  "io"
  "os"
  "strings"
  "sync"
  "time"
  "unicode/utf8"
)

// ErrInterrupted is returned by ReadLineWithGhostText when the user
// presses Ctrl-C while editing the current line — distinct from
// quitting the program: the caller's loop should just show a fresh
// prompt, the same way most shells treat Ctrl-C on a partial line.
var ErrInterrupted = errors.New("input interrupted")

// suggestDebounce is how long ReadLineWithGhostText waits after the
// last keystroke before asking for a ghost-text suggestion — long
// enough that a fast typist doesn't trigger a request per
// character, short enough to feel responsive once they pause.
const suggestDebounce = 300 * time.Millisecond

// SuggestFunc computes a ghost-text continuation for the given
// partial input, or "" for none. Called from a background goroutine
// after a typing pause; ctx is canceled if the input changes again
// before it returns, or the line is submitted/abandoned.
type SuggestFunc func(ctx context.Context, partial string) string

// lineState is the pure, terminal-independent line-editing state: a
// buffer plus the ghost-text suggestion currently shown for it.
// Kept separate from the raw terminal I/O below so it can be unit
// tested without a real TTY.
type lineState struct {
  buf        []rune
  suggestion []rune
}

type outcome struct {
  submit, interrupted, eof, changed bool
}

// apply mutates the line state for one decoded key and reports what
// happened. Typing over a shown suggestion, or backspacing, clears
// it — a suggestion is only valid for the exact buffer it was
// computed against.
//
// keyNone is deliberately NOT a case here: it's the natural Go
// zero-value of controlKey, and decodeKey never sets it on
// purpose — it's what an ordinary printable-rune key has, because
// decodeKey only ever sets .control for actual control keys. A
// case keyNone here would silently swallow every typed character.
func (s *lineState) apply(k key) outcome {
  switch k.control {
  case keyEnter:
    return outcome{ submit: true }

  case keyBackspace:
    if len(s.buf) == 0 {
      return outcome{}
    }
    s.buf = s.buf[:len(s.buf) - 1]
    s.suggestion = nil
    return outcome{ changed: true }

  case keyTab:
    if len(s.suggestion) == 0 {
      return outcome{}
    }
    s.buf = append(s.buf, s.suggestion...)
    s.suggestion = nil
    return outcome{ changed: true }

  case keyInterrupt:
    return outcome{ interrupted: true }

  case keyEOF:
    if len(s.buf) == 0 {
      return outcome{ eof: true }
    }
    return outcome{}

  case keyIgnored:
    return outcome{}
  }

  s.buf = append(s.buf, k.r)
  s.suggestion = nil
  return outcome{ changed: true }
}

func (s *lineState) text() string { return string(s.buf) }

func (s *lineState) setSuggestion(sug string) {
  s.suggestion = []rune(sug)
}

// render splits the current line into what's actually been typed
// (prompt + buf) and the dim suggestion tail shown after it.
func (s *lineState) render(prompt string) (visible, ghost string) {
  return prompt + string(s.buf), string(s.suggestion)
}

// ReadLineWithGhostText reads one line from stdin, showing a dim
// inline suggestion (from suggest) that Tab accepts. It only
// switches the terminal into raw mode — and so only supports Tab-
// accept and linear typing, not arrow-key cursor movement or
// history — when suggest is non-nil and stdin is a real terminal;
// otherwise it falls back to a plain buffered read, identical to
// the terminal's own normal line editing. That fallback is what
// runs for piped input, scripts, and tests, and whenever the
// caller hasn't turned ghost text on.
func ReadLineWithGhostText(
  ctx context.Context, reader *bufio.Reader, prompt string,
  suggest SuggestFunc,
) (string, error) {
  if suggest == nil || !isTTY(os.Stdin) {
    return readLinePlain(reader, prompt)
  }

  fd := int(os.Stdin.Fd())
  state, err := enableRawMode(fd)
  if err != nil {
    return readLinePlain(reader, prompt)
  }
  defer restoreMode(fd, state)

  return editLine(ctx, reader, prompt, suggest)
}

func readLinePlain(reader *bufio.Reader, prompt string) (string, error) {
  fmt.Print(prompt)
  line, err := reader.ReadString('\n')
  if err != nil && line == "" {
    return "", err
  }
  return strings.TrimRight(line, "\r\n"), nil
}

// editLine is the raw-mode key-by-key loop backing
// ReadLineWithGhostText. Debouncing is done with a generation
// counter, not by canceling/rescheduling a timer: a scheduled
// suggestion request only applies its result if its generation is
// still current when it completes.
func editLine(
  ctx context.Context, reader *bufio.Reader, prompt string,
  suggest SuggestFunc,
) (string, error) {
  // Leading newlines go out once: redraw reprints the prompt on
  // every keystroke from the start of the line.
  if trimmed := strings.TrimLeft(prompt, "\n"); trimmed != prompt {
    fmt.Print(prompt[:len(prompt) - len(trimmed)])
    prompt = trimmed
  }

  var (
    mu    sync.Mutex
    state lineState
    gen   int
  )

  var cancelPrev context.CancelFunc = func() {}
  defer func() { cancelPrev() }()

  redraw := func() {
    mu.Lock()
    visible, ghost := state.render(prompt)
    mu.Unlock()

    fmt.Print("\r\x1b[K" + visible + Dim(ghost))
    if n := utf8.RuneCountInString(ghost); n > 0 {
      fmt.Printf("\x1b[%dD", n)
    }
  }
  redraw()

  scheduleSuggest := func() {
    cancelPrev()
    suggestCtx, cancel := context.WithCancel(ctx)
    cancelPrev = cancel

    mu.Lock()
    gen++
    myGen := gen
    partial := state.text()
    mu.Unlock()
    if partial == "" {
      return
    }

    go func() {
      select {
      case <-time.After(suggestDebounce):
      case <-suggestCtx.Done():
        return
      }
      sug := suggest(suggestCtx, partial)
      if sug == "" || suggestCtx.Err() != nil {
        return
      }

      mu.Lock()
      stale := gen != myGen || state.text() != partial
      if !stale {
        state.setSuggestion(sug)
      }
      mu.Unlock()

      if !stale {
        redraw()
      }
    }()
  }

  for {
    k, err := decodeKey(reader)
    if err != nil {
      fmt.Print("\r\n")
      return "", err
    }

    mu.Lock()
    out := state.apply(k)
    mu.Unlock()

    switch {
    case out.submit:
      fmt.Print("\r\n")
      mu.Lock()
      line := state.text()
      mu.Unlock()
      return line, nil
    case out.interrupted:
      fmt.Print("\r\n")
      return "", ErrInterrupted
    case out.eof:
      fmt.Print("\r\n")
      return "", io.EOF
    }

    redraw()
    if out.changed {
      scheduleSuggest()
    }
  }
}
