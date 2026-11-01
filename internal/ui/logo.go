// The startup logo: the mark as an image where the terminal shows one
// (logoimage.go), else the wings mark and the wordmark as terminal
// pixels.
// Each character cell holds two square pixels, the top drawn with the
// upper half block and the bottom with the lower, so the 16-row grid
// below prints as 8 lines. White prints in the terminal's own text
// color, so the mark reads on dark and light themes alike; the
// feathers keep the mark's two greys.
//
//   Logo        print the mark, when the terminal can show it
//   logoLines   the grid as styled lines

package ui

import (
  "fmt"
  "os"
  "strings"
)

// logoGrid is the mark, one character per pixel: "#" the mark's
// white, "g" its dark grey, "G" its light grey, "." nothing. Drawn by
// hand from assets/logo_dark.svg: 3-pixel strokes on even stair
// steps, the chevron one pixel off the diagonal all the way down and
// mirrored around its apex, the wordmark with 2-pixel stems.
var logoGrid = []string{
  "##################......................................................",
  ".##################.........................................##..........",
  "..##################........................................##..........",
  "................#####...................................................",
  "...gggggggggggggg.###.........######...##.##..##.###..###...##..##.####.",
  "....ggggggggggggg..###.......###..###..#####..############..##..########",
  ".....ggggggggggg....###...........###..###....###..###..##..##..###...##",
  ".................##.###.......#######..##.....##...##...##..##..##....##",
  "......GGGGGGGGG.####.###.....###..###..##.....##...##...##..##..##....##",
  ".......GGGGGGG.######.###....##...###..##.....##...##...##..##..##....##",
  ".........GGGG.###..###.###...##...###..##.....##...##...##..##..##....##",
  "..............###..###.###...###.####..##.....##...##...##..##..##....##",
  "........GGGG.###....###.###...####.##..##.....##...##...##..##..##....##",
  ".........GG.###......###.###............................................",
  "...........###........###.###...........................................",
  "...........###........###.###...........................................",
}

// logoColors are the greys as 24-bit color, foreground and
// background, and logoColors256 the nearest of the 256 colors, for a
// terminal that doesn't say it has 24-bit (COLORTERM).
var (
  logoColors = map[byte][2]string{
    'g': { "38;2;114;113;113", "48;2;114;113;113" },
    'G': { "38;2;154;153;152", "48;2;154;153;152" },
  }
  logoColors256 = map[byte][2]string{
    'g': { "38;5;243", "48;5;243" },
    'G': { "38;5;247", "48;5;247" },
  }
)

// Logo prints the mark above the banner: only with color on, a UTF-8
// locale (the half blocks are UTF-8; every monospace font has them,
// and most terminals draw them without one), and a terminal wide
// enough for all of it.
func Logo() {
  if !Enabled || !utf8Locale() || termCols() < len(logoGrid[0]) {
    return
  }

  if logoImage() {
    fmt.Println()
    return
  }

  colors := logoColors256
  ct := os.Getenv("COLORTERM")
  if ct == "truecolor" || ct == "24bit" {
    colors = logoColors
  }

  for _, line := range logoLines(colors) {
    fmt.Println(line)
  }

  fmt.Println()
}

// utf8Locale says whether the locale's charset is UTF-8: the first of
// LC_ALL, LC_CTYPE and LANG that is set decides, as in C.
func utf8Locale() bool {
  for _, name := range []string{ "LC_ALL", "LC_CTYPE", "LANG" } {
    v := os.Getenv(name)
    if v != "" {
      v = strings.ToLower(v)
      return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
    }
  }

  return false
}

// logoLines turns each pair of grid rows into one line of half
// blocks in the given colors, a style code only where the style
// changes.
func logoLines(colors map[byte][2]string) []string {
  var out []string
  for y := 0; y + 1 < len(logoGrid); y += 2 {
    top, bottom := logoGrid[y], logoGrid[y + 1]

    // up to the pair's last pixel: no trailing blanks
    end := max(
      len(strings.TrimRight(top, ".")), len(strings.TrimRight(bottom, ".")),
    )

    var b strings.Builder
    style := ""
    for x := 0; x < end; x++ {
      glyph, want := logoCell(colors, top[x], bottom[x])
      if want != style {
        b.WriteString(reset)
        if want != "" {
          b.WriteString("\x1b[" + want + "m")
        }

        style = want
      }

      b.WriteString(glyph)
    }

    b.WriteString(reset)
    out = append(out, b.String())
  }

  return out
}

// logoCell is one cell: the glyph for its two pixels and its style
// (the codes inside "\x1b[...m", "" for the terminal's own colors).
func logoCell(colors map[byte][2]string, top, bottom byte) (string, string) {
  switch {
  case top == '.' && bottom == '.':
    return " ", ""
  case top == bottom:
    return "█", colors[top][0]
  case bottom == '.':
    return "▀", colors[top][0]
  case top == '.':
    return "▄", colors[bottom][0]
  case top == '#':
    // white over grey: the grey is the cell's background
    return "▀", "39;" + colors[bottom][1]
  case bottom == '#':
    return "▄", "39;" + colors[top][1]
  }

  // two greys: the top one is the background
  return "▄", colors[bottom][0] + ";" + colors[top][1]
}
