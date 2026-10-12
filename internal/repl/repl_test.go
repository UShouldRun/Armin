package repl

import (
  "bufio"
  "strings"
  "testing"

  "armin/internal/agent"
)

func TestSlashlessAddDir(t *testing.T) {
  dir := t.TempDir()
  if cmd, ok := slashless("add-dir " + dir); !ok || cmd != "/add-dir " + dir {
    t.Fatalf("slashless = %q, %v", cmd, ok)
  }

  if _, ok := slashless("add-dir support for the parser"); ok {
    t.Fatal("a sentence was taken for a command")
  }
}

func TestAllDoesNotCoverPrivileged(t *testing.T) {
  r := &REPL{ reader: bufio.NewReader(strings.NewReader("a\na\n")) }

  if ok, _ := r.approve(agent.ApprovePrivileged, "$ rm -rf build"); !ok || r.state & flagAutoApprove != 0 {
    t.Fatalf("privileged: ok=%v, auto-approve turned on=%v", ok, r.state & flagAutoApprove != 0)
  }

  if ok, _ := r.approve("run_program", "$ make"); !ok || r.state & flagAutoApprove == 0 {
    t.Fatal("\"a\" on an ordinary run should approve the rest of the session")
  }
}
