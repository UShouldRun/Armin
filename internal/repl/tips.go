// The rotating tips shown under the spinner while a turn runs.

package repl

import (
  "context"
  "fmt"
  "sync/atomic"
  "time"

  "armin/internal/ui"
)

// replTips rotate in the status line while the model works; /tips
// lists them all.
var replTips = []string{
  "/compact summarizes older turns to free context",
  "/context shows how full the context window is",
  "/model lists models; /model <name> switches",
  "/clear starts the conversation over",
  "Ctrl-C stops this turn without leaving armin",
  "/thinking streams the model's reasoning",
  "/approve toggles asking before each file write",
  "/remember <text> saves a note across sessions",
  "/memory lists what armin remembers",
  "/skills lists skills; /<skill> [task] runs one",
  "/add-dir <dir> lets armin read outside the project",
  "/sessions lists saved sessions; /resume <id> switches",
  "/scratch lists the model's scratchpad notes",
  "/ghosttext toggles completion while you type",
  "/suggest toggles follow-up suggestions",
  "/title <text> renames this session",
  "armin started next to nvim's :ArminLsp joins its chat",
  "/tips lists every tip",
}

// attachedTips are for a TUI sharing nvim's chat.
var attachedTips = []string{
  "/clear starts the shared conversation over, nvim's too",
  "/model lists models; /model <name> switches nvim's armin too",
  "Ctrl-C stops the answer, whoever asked",
  "questions from nvim's :ArminAsk show up here as they run",
  ":ArminAsk in nvim opens this same conversation",
  "/history reprints the conversation",
  "this chat is read-only like :ArminAsk; armin -new edits files",
  "/quit leaves; nvim's armin keeps running",
  "/tips lists every tip",
}

const (
  // tipAfter: a quick step shows no tip; tipEvery rotates them.
  tipAfter = 3 * time.Second
  tipEvery = 12 * time.Second
)

// tipTurn advances once per turn, so each turn opens on a new tip.
var tipTurn atomic.Uint32

// tipAt is the tip to show elapsed into a turn, or "".
func tipAt(tips []string, base uint32, elapsed time.Duration) string {
  if len(tips) == 0 || elapsed < tipAfter {
    return ""
  }

  i := (int(base) + int((elapsed - tipAfter) / tipEvery)) % len(tips)

  return tips[i]
}

func printTips(tips []string) {
  for i, t := range tips {
    fmt.Printf("  %s %s\n", ui.Dim(fmt.Sprintf("%2d.", i + 1)), t)
  }
}

func cmdTips(_ *REPL, _ context.Context, _ []string) {
  printTips(replTips)
}
