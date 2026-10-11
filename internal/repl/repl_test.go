package repl

import "testing"

func TestSlashlessAddDir(t *testing.T) {
  dir := t.TempDir()
  if cmd, ok := slashless("add-dir " + dir); !ok || cmd != "/add-dir " + dir {
    t.Fatalf("slashless = %q, %v", cmd, ok)
  }

  if _, ok := slashless("add-dir support for the parser"); ok {
    t.Fatal("a sentence was taken for a command")
  }
}
