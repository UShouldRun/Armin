// The -resume menu: pickSession lists saved sessions (internal/session)
// and reads a number, an id or Enter, before the REPL owns stdin.

package input

import (
  "fmt"
  "os"
  "path/filepath"
  "strconv"
  "strings"

  "armin/internal/repl"
  "armin/internal/session"
  "armin/internal/ui"
)

// pickSession shows the saved sessions and reads a choice: a menu
// number, an id (or prefix), or Enter for a new session.
func pickSession(root string) (*session.Saved, error) {
  list, err := session.List()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in pickSession:\n\t%w", err)
  }

  if len(list) == 0 {
    ui.Info("no saved sessions yet — starting a new one")
    return nil, nil
  }

  abs, _ := filepath.Abs(root)
  fmt.Println(ui.Bold("resume a session") + ui.Dim("  (* = this directory)"))
  repl.PrintSessions(list, abs, "", true)
  fmt.Print("\nnumber or id, Enter for a new session: ")

  answer, _ := ui.ReadLineUnbuffered(os.Stdin)
  line := strings.TrimSpace(answer)
  if line == "" {
    return nil, nil
  }

  n, err := strconv.Atoi(line)
  if err == nil {
    if n < 1 || n > min(len(list), 25) {
      return nil, fmt.Errorf(
        "\n\t... in pickSession:\n\t no session %d in the list", n,
      )
    }

    return session.Load(list[n - 1].ID)
  }

  return session.Load(line)
}
