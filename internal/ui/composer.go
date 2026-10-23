// The composer: an input bar under a running turn, so the user can
// type the model's next instruction while it works (agent Hooks.Steer).

package ui

import (
  "fmt"
  "os"
  "strings"
  "time"
)

// composerCheck is how often the bar checks the terminal's size.
const composerCheck = 300 * time.Millisecond

// StartComposer puts an input bar on the last row, keeps the rows
// above it for output, and reads keys from pump into it; Enter calls
// send with the line. Nil without a terminal: then nothing changes.
func StartComposer(pump *KeyPump, hint string, send func(string)) *Composer {
  if pump == nil || !isTTY(os.Stdin) || !isTTY(os.Stdout) || termRows() < 4 {
    return nil
  }

  fd := int(os.Stdin.Fd())
  mode, err := enableCbreak(fd)
  if err != nil {
    return nil
  }

  c := &Composer{
    pump:   pump,
    send:   send,
    hint:   hint,
    mode:   mode,
    stop:   make(chan struct{}),
    done:   make(chan struct{}),
    pause:  make(chan chan struct{}),
    resume: make(chan struct{}),
  }

  // Make room: if the cursor is on the last row, the region would
  // leave it outside, where output would overwrite the bar.
  fmt.Print("\n\x1b[1A" + pasteOn + kittyOn)
  c.layout()
  pump.start()
  go c.run()

  return c
}

// layout fits the scroll region to the terminal's height, keeping
// the cursor where it was, and draws the bar.
func (c *Composer) layout() {
  rows := termRows()
  c.rows = rows
  fmt.Printf("\x1b7\x1b[1;%dr\x1b8", rows - 1)
  c.draw()
}

// draw paints the bar on the last row without moving the cursor.
func (c *Composer) draw() {
  c.mu.Lock()
  text, queued, paused := barText(c.state.display()), c.queued, c.paused
  c.mu.Unlock()

  width := termCols()
  bar := Prompt() + text
  switch {
  case paused:
    bar = Dim("  (answer the question above)")
  case text == "":
    bar = Prompt() + Dim(c.hint)
  }

  if queued > 0 {
    bar += Dim(fmt.Sprintf("  [%d queued]", queued))
  }

  // Keep the end of a long line in view.
  runes := []rune(text)
  if width > 10 && len(runes) > width - 6 && !paused {
    bar = Prompt() + "…" + string(runes[len(runes) - (width - 6):])
  }

  fmt.Printf("\x1b7\x1b[%d;1H\x1b[2K%s\x1b8", c.rows, bar)
}

func (c *Composer) run() {
  defer close(c.done)

  tick := time.NewTicker(composerCheck)
  defer tick.Stop()

  for {
    c.mu.Lock()
    paused := c.paused
    c.mu.Unlock()

    // Paused, no key is read here: a question reads them.
    if paused {
      select {
      case <-c.resume:
        c.mu.Lock()
        c.paused = false
        c.mu.Unlock()
        c.draw()
      case <-c.stop:
        return
      }

      continue
    }

    select {
    case <-c.stop:
      return
    case ack := <-c.pause:
      c.mu.Lock()
      c.paused = true
      c.mu.Unlock()
      c.draw()
      close(ack)
    case <-tick.C:
      if termRows() != c.rows {
        c.layout()
      }
    case r := <-c.pump.keys:
      if r.err != nil {
        return
      }

      c.key(r.k)
    }
  }
}

func (c *Composer) key(k key) {
  c.mu.Lock()
  out := c.state.apply(k)
  line := ""
  if out.submit {
    line = c.state.text()
    c.state.reset()
  }

  c.mu.Unlock()

  // With the kitty protocol on, Ctrl-C comes as a key, not as the
  // signal the terminal would send: send it ourselves.
  if out.interrupted {
    p, err := os.FindProcess(os.Getpid())
    if err == nil {
      p.Signal(os.Interrupt)
    }
  }

  if out.submit && line != "" {
    c.mu.Lock()
    c.queued++
    c.mu.Unlock()
    c.send(line)
  }

  c.draw()
}

// Taken says the turn picked up n queued messages.
func (c *Composer) Taken(n int) {
  if c == nil {
    return
  }

  c.mu.Lock()
  c.queued = max(0, c.queued - n)
  c.mu.Unlock()
  c.draw()
}

// Pause lets a question read the keys, once the bar has stopped
// reading them; Resume takes them back.
func (c *Composer) Pause() {
  if c == nil {
    return
  }

  ack := make(chan struct{})
  select {
  case c.pause <- ack:
    <-ack
  case <-c.done:
  }
}

func (c *Composer) Resume() {
  if c == nil {
    return
  }

  select {
  case c.resume <- struct{}{}:
  case <-c.done:
  }
}

// Stop removes the bar, gives the whole screen back and returns
// what was typed but not sent, for the next prompt.
func (c *Composer) Stop() string {
  if c == nil {
    return ""
  }

  close(c.stop)
  <-c.done

  fmt.Printf("\x1b7\x1b[%d;1H\x1b[2K\x1b[r\x1b8" + kittyOff + pasteOff, c.rows)
  restoreMode(int(os.Stdin.Fd()), c.mode)

  c.mu.Lock()
  defer c.mu.Unlock()

  return c.state.text()
}

// barText is the typed text on the bar's one row: a pasted newline
// shows as ↵, a tab as a space.
func barText(buf []rune) string {
  var b strings.Builder
  for _, r := range buf {
    switch r {
    case '\n':
      b.WriteRune('↵')
    case '\t':
      b.WriteByte(' ')
    default:
      b.WriteRune(r)
    }
  }

  return b.String()
}
