// The full picture of a proposal in a new terminal window (terminal
// = in config.conf): the whole diff, or the script about to run.

package repl

import (
  "errors"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/agent"
)

// terminals armin knows how to open a window in, most likely first,
// with the arguments that come before the command to run.
var terminals = []struct {
  name string
  args []string
}{
  { "ghostty", []string{ "-e" } },
  { "kitty", nil },
  { "wezterm", []string{ "start", "--" } },
  { "alacritty", []string{ "-e" } },
  { "foot", nil },
  { "konsole", []string{ "-e" } },
  { "gnome-terminal", []string{ "--" } },
  { "x-terminal-emulator", []string{ "-e" } },
  { "xterm", []string{ "-e" } },
}

// viewAge: views older than this are cleaned up on the next one.
const viewAge = 24 * time.Hour

// terminalCmd is how to run a program in a new terminal window:
// config.conf's terminal, a tmux window inside tmux, $TERMINAL, or
// the first known terminal installed.
func terminalCmd(configured string) ([]string, error) {
  if configured != "" {
    return strings.Fields(configured), nil
  }

  if os.Getenv("TMUX") != "" {
    return []string{ "tmux", "new-window" }, nil
  }

  graphical := os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
  if !graphical {
    return nil, errors.New("no window system to open a terminal on — set terminal in config.conf (or run armin in tmux)")
  }

  if t := os.Getenv("TERMINAL"); t != "" {
    for _, known := range terminals {
      if filepath.Base(t) == known.name {
        return append([]string{ t }, known.args...), nil
      }
    }

    return []string{ t, "-e" }, nil
  }

  for _, t := range terminals {
    path, err := exec.LookPath(t.name)
    if err == nil {
      return append([]string{ path }, t.args...), nil
    }
  }

  return nil, errors.New("no terminal found — set terminal in config.conf, e.g. terminal = ghostty -e")
}

// view opens a proposal in nvim, read-only, in a new terminal
// window: a change as a diff against the file on disk, a new file
// as itself, a run as its script or its command line.
func (r *REPL) view(p agent.Proposal) error {
  nvim, err := exec.LookPath("nvim")
  if err != nil {
    return errors.New("nvim isn't on PATH")
  }

  term, err := terminalCmd(r.Terminal)
  if err != nil {
    return err
  }

  dir, err := viewDir()
  if err != nil {
    return err
  }

  args := []string{ nvim, "-R" }
  switch {
  case p.Command != "" && p.Script != "":
    args = append(args, p.Script)

  case p.Command != "":
    path := filepath.Join(dir, "command.sh")
    body := "# armin wants to run this, in " + p.Dir + "\n" + p.Command + "\n"
    err = os.WriteFile(path, []byte(body), 0o600)
    args = append(args, path)

  case p.Created:
    // The new file keeps its name, so nvim highlights it.
    path := filepath.Join(dir, filepath.Base(p.Path))
    err = os.WriteFile(path, []byte(p.New), 0o600)
    args = append(args, path)

  default:
    // One window: the whole file as a diff, opened at the change.
    path := filepath.Join(dir, filepath.Base(p.Path) + ".diff")
    text, at := fileDiff(p)
    err = os.WriteFile(path, []byte(text), 0o600)
    args = append(args, "-c", "setlocal filetype=diff", fmt.Sprintf("+%d", at), path)
  }

  if err != nil {
    return fmt.Errorf("\n\t... in view:\n\t%w", err)
  }

  cmd := exec.Command(term[0], append(term[1:], args...)...)
  cmd.Dir = r.Root
  err = cmd.Start()
  if err != nil {
    return fmt.Errorf("\n\t... in view:\n\t starting %s: %w", term[0], err)
  }

  go cmd.Wait() // reap it; the window lives on its own

  return nil
}

// viewDir is a fresh directory for one view's files; ones left from
// views over a day old go first.
func viewDir() (string, error) {
  old, _ := filepath.Glob(filepath.Join(os.TempDir(), "armin-view-*"))
  for _, d := range old {
    info, err := os.Stat(d)
    if err == nil && time.Since(info.ModTime()) > viewAge {
      os.RemoveAll(d)
    }
  }

  return os.MkdirTemp("", "armin-view-")
}

// readSmall reads a file to show, or "" if it can't.
func readSmall(path string) string {
  data, err := os.ReadFile(path)
  if err != nil {
    return ""
  }

  return string(data)
}

// fileDiff is the whole file with the change in place, as a unified
// diff: " " kept, "-" removed, "+" added. at is the line of the first
// change, to open at.
func fileDiff(p agent.Proposal) (text string, at int) {
  var b strings.Builder
  added, removed := len(p.Came), len(p.Gone)
  fmt.Fprintf(&b, "--- %s\n+++ %s (proposed)\n", p.Path, p.Path)
  fmt.Fprintf(&b, "@@ -1,%d +1,%d @@ %d additions, %d removals\n",
    len(p.OldLines), len(p.OldLines) - removed + added, added, removed)

  for _, l := range p.OldLines[:p.Start] {
    b.WriteString(" " + l + "\n")
  }

  for _, l := range p.Gone {
    b.WriteString("-" + l + "\n")
  }

  for _, l := range p.Came {
    b.WriteString("+" + l + "\n")
  }

  for _, l := range p.OldLines[p.Start + removed:] {
    b.WriteString(" " + l + "\n")
  }

  return b.String(), 3 + p.Start + 1
}
