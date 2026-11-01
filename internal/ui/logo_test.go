package ui

import (
  "strings"
  "testing"
)

func TestLogoGrid(t *testing.T) {
  if len(logoGrid) % 2 != 0 {
    t.Fatal("the grid needs an even number of rows: two per line")
  }

  for i, row := range logoGrid {
    if len(row) != len(logoGrid[0]) {
      t.Fatalf("row %d is %d wide, not %d", i, len(row), len(logoGrid[0]))
    }

    if strings.Trim(row, ".#gG") != "" {
      t.Fatalf("row %d has a pixel that isn't . # g or G: %q", i, row)
    }
  }
}

func TestLogoLines(t *testing.T) {
  lines := logoLines(logoColors)
  if len(lines) != len(logoGrid) / 2 {
    t.Fatalf("%d lines, want %d", len(lines), len(logoGrid) / 2)
  }

  for i, l := range lines {
    plain := ansi.ReplaceAllString(l, "")
    if strings.Trim(plain, " ▀▄█") != "" {
      t.Fatalf("line %d holds more than half blocks: %q", i, plain)
    }

    if len([]rune(plain)) > len(logoGrid[0]) {
      t.Fatalf("line %d is wider than the grid", i)
    }

    if !strings.HasSuffix(l, reset) {
      t.Fatalf("line %d doesn't end its color", i)
    }
  }

  // white over light grey: the cell's background is the grey
  glyph, style := logoCell(logoColors, '#', 'G')
  if glyph != "▀" || !strings.Contains(style, "48;2;154;153;152") {
    t.Fatalf("white over grey = %q %q", glyph, style)
  }
}

func TestLogoNeedsColor(t *testing.T) {
  was := Enabled
  Enabled = false
  defer func() { Enabled = was }()

  out := capture(t, func() { Logo() })
  if out != "" {
    t.Fatalf("no color, no logo: %q", out)
  }
}

func TestLogoLocaleAndColors(t *testing.T) {
  t.Setenv("LC_ALL", "")
  t.Setenv("LC_CTYPE", "C")
  t.Setenv("LANG", "en_US.UTF-8")
  if utf8Locale() {
    t.Fatal("LC_CTYPE=C decides before LANG: not UTF-8")
  }

  t.Setenv("LC_CTYPE", "")
  if !utf8Locale() {
    t.Fatal("LANG=en_US.UTF-8 is UTF-8")
  }

  plain := strings.Join(logoLines(logoColors256), "")
  if strings.Contains(plain, "38;2;") || !strings.Contains(plain, "38;5;") {
    t.Fatal("the 256-color logo should use only 256 colors")
  }
}
