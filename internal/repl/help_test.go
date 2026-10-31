package repl

import (
  "context"
  "io"
  "os"
  "strings"
  "testing"
)

// TestHelpColumns: no line of /help runs wide because of one long
// usage; the long one breaks onto its own line instead.
func TestHelpColumns(t *testing.T) {
  read, write, _ := os.Pipe()
  stdout := os.Stdout
  os.Stdout = write
  cmdHelp(&REPL{}, context.Background(), nil)
  write.Close()
  os.Stdout = stdout
  out, _ := io.ReadAll(read)

  for _, line := range strings.Split(string(out), "\n") {
    usage, summary, found := strings.Cut(strings.TrimSpace(line), "  ")
    if found && strings.HasPrefix(usage, "/") &&
      len(line) - len(strings.TrimLeft(summary, " ")) > 2 + helpWidth + 2 + len(summary) {
      t.Fatalf("summary pushed right: %q", line)
    }
  }

  if !strings.Contains(string(out), "  /library [index") {
    t.Fatal("the long usage is missing")
  }
}
