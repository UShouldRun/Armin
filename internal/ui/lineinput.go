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

// A paste longer than this many lines or runes is folded into a
// label: drawn whole, it could be taller than the terminal, which
// the redraw can't climb back over.
const (
  foldLines = 8
  foldRunes = 1000
)

// pasteBase is where folded pastes' runes start, in the last
// private-use plane; typed or pasted runes up there are dropped.
const pasteBase rune = 0x100000

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

    s.truncate(len(s.buf) - 1)

    return outcome{ changed: true }

  case keyKillLine:
    if len(s.buf) == 0 {
      return outcome{}
    }

    s.reset()

    return outcome{ changed: true }

  case keyKillWord:
    // Like a shell: trailing spaces, then the word before them.
    n := len(s.buf)
    for n > 0 && isSpace(s.buf[n - 1]) {
      n--
    }

    for n > 0 && !isSpace(s.buf[n - 1]) {
      n--
    }

    if n == len(s.buf) {
      return outcome{}
    }

    s.truncate(n)

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

  case keyPaste:
    if len(k.paste) == 0 {
      return outcome{}
    }

    s.paste(k.paste)

    return outcome{ changed: true }
  }

  if k.r >= pasteBase {
    return outcome{}
  }

  s.buf = append(s.buf, k.r)
  s.suggestion = nil

  return outcome{ changed: true }
}

func isSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\t' }

// paste appends p, folded into one rune when it's large.
func (s *lineState) paste(p []rune) {
  s.suggestion = nil

  lines := 1
  for _, r := range p[:len(p) - 1] {
    if r == '\n' {
      lines++
    }
  }

  if lines <= foldLines && len(p) <= foldRunes {
    for _, r := range p {
      if r < pasteBase {
        s.buf = append(s.buf, r)
      }
    }

    return
  }

  s.buf = append(s.buf, pasteBase + rune(len(s.pastes)))
  s.pastes = append(s.pastes, p)
}

// truncate cuts buf to n runes, letting go of the pastes cut with
// it.
func (s *lineState) truncate(n int) {
  s.buf, s.suggestion = s.buf[:n], nil

  kept := 0
  for _, r := range s.buf {
    if r >= pasteBase {
      kept = int(r - pasteBase) + 1
    }
  }

  clear(s.pastes[kept:])
  s.pastes = s.pastes[:kept]
}

func (s *lineState) reset() { s.truncate(0) }

// expand is buf with each folded paste swapped for its text, or,
// with labels, for the label shown in its place.
func (s *lineState) expand(labels bool) []rune {
  out := make([]rune, 0, len(s.buf))
  for _, r := range s.buf {
    if r < pasteBase {
      out = append(out, r)
      continue
    }

    p := s.pastes[r - pasteBase]
    if !labels {
      out = append(out, p...)
      continue
    }

    out = append(out, []rune(pasteLabel(p))...)
  }

  return out
}

func pasteLabel(p []rune) string {
  lines := 1
  for _, r := range p[:len(p) - 1] {
    if r == '\n' {
      lines++
    }
  }

  if lines == 1 {
    return fmt.Sprintf("[pasted %d chars]", len(p))
  }

  return fmt.Sprintf("[pasted %d lines]", lines)
}

// text is what was typed, pastes and all; display is what's drawn.
func (s *lineState) text() string    { return string(s.expand(false)) }
func (s *lineState) display() []rune { return s.expand(true) }

func (s *lineState) setSuggestion(sug string) {
  s.suggestion = []rune(sug)
}

// render splits the current line into what's actually been typed
// (prompt + buf) and the dim suggestion tail shown after it.
func (s *lineState) render(prompt string) (visible, ghost string) {
  return prompt + string(s.display()), string(s.suggestion)
}

// ReadLineWithGhostText reads one line from stdin. On a terminal it
// edits the line in raw mode — linear typing, Backspace, Ctrl-U,
// Ctrl-W, no arrow-key movement or history, pastes taken whole with
// their newlines — wrapping it at
// TextWidth, and, when suggest is non-nil, shows a dim inline
// suggestion that Tab accepts. Without a terminal (piped input,
// scripts, tests) it falls back to a plain buffered read.
func ReadLineWithGhostText(
  ctx context.Context, reader *bufio.Reader, prompt string,
  suggest SuggestFunc,
) (string, error) {
  // The raw editor runs on every terminal, ghost text or not: it is
  // what wraps the input at TextWidth.
  if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
    return readLinePlain(reader, prompt)
  }

  fd := int(os.Stdin.Fd())
  state, err := enableRawMode(fd)
  if err != nil {
    return readLinePlain(reader, prompt)
  }

  defer restoreMode(fd, state)

  fmt.Print(pasteOn)
  defer fmt.Print(pasteOff)

  read := func() (key, error) { return decodeKey(reader) }

  return editLine(ctx, read, prompt, suggest, "")
}

// ErrWoken is ReadLineWake's return when wake fired: the input area
// was cleared, and the text typed so far comes back with it.
var ErrWoken = errors.New("input woken")

func NewKeyPump(reader *bufio.Reader) *KeyPump {
  return &KeyPump{ reader: reader, keys: make(chan keyRead, 64) }
}

func (p *KeyPump) start() {
  p.once.Do(func() {
    go func() {
      for {
        k, err := decodeKey(p.reader)
        p.keys <- keyRead{ k, err }
        if err != nil {
          return
        }
      }
    }()
  })
}

// ReadLineWake is ReadLineWithGhostText for a prompt that other
// output may interrupt: when wake fires, it clears what it drew and
// returns the typed text with ErrWoken, to be passed back as initial
// once the output is done.
func ReadLineWake(
  ctx context.Context, pump *KeyPump, prompt, initial string,
  wake <-chan struct{}, suggest SuggestFunc,
) (string, error) {
  if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
    return readLinePlain(pump.reader, prompt)
  }

  fd := int(os.Stdin.Fd())
  state, err := enableRawMode(fd)
  if err != nil {
    return readLinePlain(pump.reader, prompt)
  }

  defer restoreMode(fd, state)

  fmt.Print(pasteOn)
  defer fmt.Print(pasteOff)

  pump.start()
  read := func() (key, error) {
    select {
    case r := <-pump.keys:
      return r.k, r.err
    case <-wake:
      return key{}, ErrWoken
    case <-ctx.Done():
      return key{}, ctx.Err()
    }
  }

  return editLine(ctx, read, prompt, suggest, initial)
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
  ctx context.Context, read func() (key, error), prompt string,
  suggest SuggestFunc, initial string,
) (string, error) {
  // Leading newlines go out once: redraw reprints the prompt on
  // every keystroke from the start of the line.
  trimmed := strings.TrimLeft(prompt, "\n")
  if trimmed != prompt {
    fmt.Print(prompt[:len(prompt) - len(trimmed)])
    prompt = trimmed
  }

  var (
    mu    sync.Mutex
    state lineState
    gen   int
  )

  // Text handed back from before (see ReadLineWake) is pasted, so
  // a large paste in it folds again.
  if initial != "" {
    state.paste([]rune(initial))
  }

  var cancelPrev context.CancelFunc = func() {}
  defer func() { cancelPrev() }()

  // The input wraps at the same width as the answers, continuation
  // rows lining up under the text after the prompt. row is where the
  // cursor sits, counted from the prompt's row, so a redraw can go
  // back up to it.
  // The terminal's width is read on every redraw: a window manager
  // can resize it while the line is open (i3 tiling a new window).
  measure := func() (width, promptW, hang int) {
    width = termCols()
    if width <= 0 {
      width = 80
    }

    width--
    promptW = visibleWidth(prompt)
    hang = promptW
    if width - promptW < 20 {
      hang = 0
    }

    return width, promptW, hang
  }

  // A prompt that leaves no room to type goes on its own line, and
  // the typing on a short one below it: wrapped, it would throw the
  // redraws off.
  width, promptW, _ := measure()
  if promptW > width - 20 {
    fmt.Print(prompt + "\r\n")
    prompt = Prompt()
  }

  row := 0
  redraw := func() {
    mu.Lock()
    text := state.display()
    typed := len(text)
    text = append(text, state.suggestion...)
    mu.Unlock()

    width, promptW, hang := measure()
    rows := layoutInput(text, max(1, width - promptW), max(1, width - hang))

    var b strings.Builder
    if row > 0 {
      fmt.Fprintf(&b, "\x1b[%dA", row)
    }

    b.WriteString("\r\x1b[J" + prompt)
    at := 0
    for i, r := range rows {
      if i > 0 {
        b.WriteString("\r\n" + strings.Repeat(" ", hang))
      }

      // Runes past what was typed are the suggestion, shown dim.
      split := min(max(typed - at, 0), len(r))
      b.WriteString(inputText(r[:split]))
      if split < len(r) {
        b.WriteString(Dim(inputText(r[split:])))
      }

      at += len(r)
    }

    cr, cc := cursorAt(rows, typed)
    up := len(rows) - 1 - cr
    if up > 0 {
      fmt.Fprintf(&b, "\x1b[%dA", up)
    }

    left := hang
    if cr == 0 {
      left = promptW
    }

    b.WriteString("\r")
    if left + cc > 0 {
      fmt.Fprintf(&b, "\x1b[%dC", left + cc)
    }

    row = cr
    fmt.Print(b.String())
  }

  redraw()

  // done leaves the line as typed, without a suggestion, and moves
  // below it.
  done := func() {
    mu.Lock()
    state.suggestion = nil
    mu.Unlock()
    redraw()
    fmt.Print("\r\n")
  }

  scheduleSuggest := func() {
    cancelPrev()
    suggestCtx, cancel := context.WithCancel(ctx)
    cancelPrev = cancel

    mu.Lock()
    gen++
    myGen := gen
    partial := state.text()
    mu.Unlock()
    if partial == "" || suggest == nil {
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
    k, err := read()
    if err == ErrWoken {
      if row > 0 {
        fmt.Printf("\x1b[%dA", row)
      }

      fmt.Print("\r\x1b[J")
      mu.Lock()
      line := state.text()
      mu.Unlock()

      return line, ErrWoken
    }

    if err != nil {
      done()
      return "", err
    }

    mu.Lock()
    out := state.apply(k)
    mu.Unlock()

    switch {
    case out.submit:
      done()
      mu.Lock()
      line := state.text()
      mu.Unlock()

      return line, nil
    case out.interrupted:
      done()

      return "", ErrInterrupted
    case out.eof:
      done()

      return "", io.EOF
    }

    redraw()
    if out.changed {
      scheduleSuggest()
    }
  }
}

// inputText is how a row of input prints: one column per rune, so a
// tab shows as a space, and a row's closing newline isn't printed —
// the row break is.
func inputText(row []rune) string {
  var b strings.Builder
  for _, r := range row {
    switch r {
    case '\n':
    case '\t':
      b.WriteByte(' ')
    default:
      b.WriteRune(r)
    }
  }

  return b.String()
}

// ReadLineUnbuffered reads up to a newline a byte at a time, for a
// question asked before the REPL starts: a buffered reader would
// keep keys meant for the prompt that comes next.
func ReadLineUnbuffered(r io.Reader) (string, error) {
  var b strings.Builder
  var c [1]byte
  for {
    n, err := r.Read(c[:])
    if n == 1 {
      if c[0] == '\n' {
        return b.String(), nil
      }

      b.WriteByte(c[0])
    }

    if err != nil {
      return b.String(), err
    }
  }
}
