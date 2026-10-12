package repl

import (
  "bufio"
  "io"
  "os"
  "strings"
  "testing"
  "time"

  "armin/internal/agent"
  "armin/internal/ui"
)

func TestSlashlessAddDir(t *testing.T) {
  dir := t.TempDir()
  cmd, ok := slashless("add-dir " + dir)
  if !ok || cmd != "/add-dir " + dir {
    t.Fatalf("slashless = %q, %v", cmd, ok)
  }

  _, ok = slashless("add-dir support for the parser")
  if ok {
    t.Fatal("a sentence was taken for a command")
  }
}

func TestAllDoesNotCoverPrivileged(t *testing.T) {
  r := &REPL{ reader: bufio.NewReader(strings.NewReader("a\na\n")) }

  ok, _ := r.approve(agent.ApprovePrivileged, "$ rm -rf build")
  if !ok || r.state & flagAutoApprove != 0 {
    t.Fatalf("privileged: ok=%v, auto-approve turned on=%v", ok, r.state & flagAutoApprove != 0)
  }

  ok, _ = r.approve("run_program", "$ make")
  if !ok || r.state & flagAutoApprove == 0 {
    t.Fatal("\"a\" on an ordinary run should approve the rest of the session")
  }
}

func TestTurnStatus(t *testing.T) {
  v := &turnView{}
  v.setPhase(phaseWaiting, true)
  s := v.status()
  if s != "waiting for the model · 0s" {
    t.Fatalf("status = %q", s)
  }

  v.progress(2048, 13906)
  s = v.status()
  if !strings.HasPrefix(s, "reading the prompt 2.0k/13k") {
    t.Fatalf("status = %q", s)
  }

  v.last = time.Now().Add(-30 * time.Second)
  s = v.status()
  if !strings.Contains(s, "nothing from the model for 30s") {
    t.Fatalf("a silent model isn't flagged: %q", s)
  }

  v.setPhase("running make", false)
  v.since = time.Now().Add(-90 * time.Second)
  v.last = v.since
  s = v.status()
  if s != "running make · 1m30s" {
    t.Fatalf("a long tool run is flagged as a silent model: %q", s)
  }
}

// capture runs f and returns what it printed to stdout.
func capture(t *testing.T, f func()) string {
  t.Helper()
  r, w, err := os.Pipe()
  if err != nil {
    t.Fatal(err)
  }

  old := os.Stdout
  os.Stdout = w
  f()
  w.Close()
  os.Stdout = old

  out, _ := io.ReadAll(r)

  return string(out)
}

// TestAnswerNewlines: what the model ends a remark on before a tool
// call prints nothing; inside an answer, one blank line at most.
func TestAnswerNewlines(t *testing.T) {
  ui.Enabled = false
  defer func() { ui.Enabled = true }()

  cases := []struct {
    name   string
    deltas []string
    tool   bool
    want   string
  }{
    { "remark then tool", []string{ "Let me verify it builds:", "\n\n\n" }, true, "Let me verify it builds:\n● build_check" },
    { "newlines in one delta", []string{ "whether it builds.\n\n\n" }, true, "whether it builds.\n● build_check" },
    { "paragraphs capped", []string{ "One.\n\n\n\nTwo." }, false, "One.\n\nTwo." },
    { "split across deltas", []string{ "One.\n", "\n", "\nTwo." }, false, "One.\n\nTwo." },
    { "space between words", []string{ "the ", "project" }, false, "the project" },
    { "single newline kept", []string{ "a\n", "b" }, false, "a\nb" },
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      v := &turnView{ wrap: ui.NewWrapper(0) }
      got := capture(t, func() {
        for _, d := range c.deltas {
          v.token(d)
        }

        if c.tool {
          v.toolCall("build_check", nil)
        } else {
          v.flush()
        }
      })

      got = strings.TrimRight(got, "\n")
      if !strings.HasPrefix(got, c.want) {
        t.Fatalf("printed %q, want it to start %q", got, c.want)
      }

      if c.tool && strings.Contains(got, "\n\n") {
        t.Fatalf("blank line before the tool: %q", got)
      }
    })
  }
}
