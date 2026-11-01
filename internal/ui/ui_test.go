package ui

import (
  "bytes"
  "image"
  "image/color"
  "image/png"
  "io"
  "os"
  "path/filepath"
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
func TestLogoKitty(t *testing.T) {
  enabled := Enabled
  Enabled = true
  defer func() { Enabled = enabled }()

  // the harness's own terminal would leak in: ghostty sets
  // TERM_PROGRAM, and detection believes it over TERM
  term := os.Getenv("TERM")
  program := os.Getenv("TERM_PROGRAM")
  defer func() {
    os.Setenv("TERM", term)
    os.Setenv("TERM_PROGRAM", program)
  }()
  os.Setenv("TERM", "xterm-kitty")
  os.Setenv("TERM_PROGRAM", "")

  path := filepath.Join(t.TempDir(), "logo.png")
  writeTestPNG(t, path)

  saved := logoPNG
  defer func() { logoPNG = saved }()

  data, err := os.ReadFile(path)
  if err != nil {
    t.Fatal(err)
  }

  logoPNG = data

  out := capture(t, func() { Logo() })

  if !strings.Contains(out, "\x1b_G") {
    t.Fatalf("kitty terminal got no image:\n%q", out)
  }

  if !strings.Contains(out, "a=T,f=100") {
    t.Errorf("kitty escape doesn't display a PNG (a=T,f=100): %q", out)
  }

  // one 4096-byte chunk at most: params and m=0 (last chunk) on it.
  // m=1 marks more-coming, so a multi-chunk image keeps the params
  // escape m=1 and ends on a bare m=0.
  if !strings.Contains(out, "m=0") {
    t.Errorf("kitty escape lacks the m=0 last chunk: %q", out)
  }

  os.Setenv("TERM", "xterm-256color")
  os.Setenv("TERM_PROGRAM", "")
  out = capture(t, func() { Logo() })

  if out != "" {
    t.Errorf("plain terminal got %q, want nothing", out)
  }
}

// writeTestPNG saves a tiny PNG at path, so the logo tests don't
// depend on the repo's own assets.
func writeTestPNG(t *testing.T, path string) {
  t.Helper()

  img := image.NewRGBA(image.Rect(0, 0, 8, 8))
  for x := 0; x < 8; x++ {
    for y := 0; y < 8; y++ {
      img.Set(x, y, color.RGBA{ 20, 20, 20, 255 })
    }
  }

  f, err := os.Create(path)
  if err != nil {
    t.Fatal(err)
  }

  defer f.Close()

  err = png.Encode(f, img)
  if err != nil {
    t.Fatalf("encoding test PNG: %s", err)
  }
}