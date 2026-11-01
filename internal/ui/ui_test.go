package ui

import (
  "bytes"
  "io"
  "os"
  "strings"
  "testing"

  "armin/internal/build"
)

// capture collects what f prints on stdout, with colors off so the
// text is plain.
func capture(t *testing.T, f func()) string {
  t.Helper()

  saved := *os.Stdout
  r, wpipe, err := os.Pipe()
  if err != nil {
    t.Fatal(err)
  }

  *os.Stdout = *wpipe

  done := make(chan string)
  go func() {
    var buf bytes.Buffer
    _, _ = io.Copy(&buf, r)
    done <- buf.String()
  }()

  f()

  err = wpipe.Close()
  if err != nil {
    t.Fatal(err)
  }

  *os.Stdout = saved

  out := <-done
  _ = r.Close()

  return out
}

// TestBanner checks the header's shape: the name and its fields,
// one line each, no matter what the logo did above them.
func TestBanner(t *testing.T) {
  enabled := Enabled
  Enabled = false
  defer func() { Enabled = enabled }()

  out := capture(t, func() { Banner("/proj", "planner", "openrouter") })
  lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")

  if len(lines) != 3 {
    t.Fatalf("Banner printed %d lines, want 3:\n%s", len(lines), out)
  }

  if !strings.Contains(
    lines[0],
    build.Name +
      " — terminal codebase assistant",
  ) {
    t.Errorf("first line %q lacks the name and tagline", lines[0])
  }

  if !strings.Contains(lines[1], "root: /proj") {
    t.Errorf("line %q lacks root", lines[1])
  }

  if lines[2] != "" {
    t.Errorf("line %q is not the blank separator", lines[2])
  }
}

// TestLogoKitty checks the kitty protocol path: a terminal that
// speaks it gets the image, one that doesn't gets nothing.
