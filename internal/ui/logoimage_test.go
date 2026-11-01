package ui

import (
  "strings"
  "testing"
)

func TestParseBackground(t *testing.T) {
  cases := []struct {
    reply     string
    light, ok bool
  }{
    { "\x1b]11;rgb:1414/1b1b/1e1e\x1b\\\x1b[?62;22c", false, true },
    { "\x1b]11;rgb:fafa/fafa/f9f9\a\x1b[?1;2c", true, true },
    { "\x1b]11;rgb:ff/ff/ff\x1b\\", true, true },
    { "\x1b[?62;22c", false, false },
    { "", false, false },
  }

  for _, c := range cases {
    light, ok := parseBackground([]byte(c.reply))
    if light != c.light || ok != c.ok {
      t.Errorf("parseBackground(%q) = %v %v", c.reply, light, ok)
    }
  }

  if !daReply.MatchString("\x1b]11;rgb:0/0/0\a\x1b[?65;1;9c") {
    t.Error("the device-attributes answer ends the read")
  }
}

func TestKittyImage(t *testing.T) {
  png := make([]byte, 7000)
  out := kittyImage(png, 8)
  escapes := strings.Split(strings.TrimSuffix(out, "\x1b\\"), "\x1b\\")
  if len(escapes) != 3 {
    t.Fatalf("%d escapes, want 3 for 7000 bytes of png", len(escapes))
  }

  if !strings.HasPrefix(escapes[0], "\x1b_Ga=T,f=100,r=8,C=1,q=2,m=1;") {
    t.Fatalf("the first escape shows the image, 8 rows, cursor kept, " +
      "replies off: %.40q", escapes[0])
  }

  if !strings.HasPrefix(escapes[1], "\x1b_Gm=1;") ||
    !strings.HasPrefix(escapes[2], "\x1b_Gm=0;") {
    t.Fatal("m=1 on every chunk but the last, m=0 on it")
  }
}

func TestGraphics(t *testing.T) {
  t.Setenv("TMUX", "")
  t.Setenv("KITTY_WINDOW_ID", "")
  t.Setenv("TERM", "xterm-ghostty")
  t.Setenv("TERM_PROGRAM", "ghostty")
  if graphics() != protoKitty {
    t.Fatal("ghostty speaks the kitty protocol")
  }

  t.Setenv("TERM", "xterm-256color")
  t.Setenv("TERM_PROGRAM", "iTerm.app")
  if graphics() != protoITerm {
    t.Fatal("iTerm2 has its own")
  }

  t.Setenv("TERM_PROGRAM", "gnome-terminal")
  if graphics() != protoNone {
    t.Fatal("no protocol, no image")
  }

  t.Setenv("TERM", "xterm-kitty")
  t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
  if graphics() != protoNone {
    t.Fatal("tmux drops the escapes: no image")
  }
}
