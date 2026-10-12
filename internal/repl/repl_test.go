package repl

import (
  "bufio"
  "strings"
  "testing"
  "time"

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

func TestTurnStatus(t *testing.T) {
  v := &turnView{}
  v.setPhase(phaseWaiting, true)
  if s := v.status(); s != "waiting for the model · 0s" {
    t.Fatalf("status = %q", s)
  }

  v.progress(2048, 13906)
  if s := v.status(); !strings.HasPrefix(s, "reading the prompt 2.0k/13k") {
    t.Fatalf("status = %q", s)
  }

  v.last = time.Now().Add(-30 * time.Second)
  if s := v.status(); !strings.Contains(s, "nothing from the model for 30s") {
    t.Fatalf("a silent model isn't flagged: %q", s)
  }

  v.setPhase("running make", false)
  v.since = time.Now().Add(-90 * time.Second)
  v.last = v.since
  if s := v.status(); s != "running make · 1m30s" {
    t.Fatalf("a long tool run is flagged as a silent model: %q", s)
  }
}
