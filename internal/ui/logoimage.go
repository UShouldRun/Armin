// The startup logo as an image, on terminals that show images: the
// mark's own PNG (made from the SVG, transparent around it), sized by
// the terminal to the pixel logo's 8 rows. Otherwise Logo falls back
// to the pixel art in logo.go.
//
//   logoImage      print the image, if the terminal can and says its
//                  background color (the mark is drawn for one)
//   graphics       which image protocol the terminal speaks
//   kittyImage     the kitty protocol (kitty, ghostty, WezTerm)
//   itermImage     iTerm2's
//   parseBackground  the terminal's answer to "what's your background?"

package ui

import (
  _ "embed"
  "encoding/base64"
  "fmt"
  "os"
  "regexp"
  "strconv"
  "strings"
)

// The mark as PNGs with a transparent background, made from
// internal/web/static/logo.svg and logo-light.svg: cream for dark
// backgrounds, dark ink for light ones.
var (
  //go:embed logo_dark.png
  logoOnDark []byte

  //go:embed logo_light.png
  logoOnLight []byte
)

// The image protocols logoImage speaks.
const (
  protoNone = iota
  protoKitty
  protoITerm
)

// kittyChunk is the base64 a kitty escape carries at most.
const kittyChunk = 4096

// logoImage prints the mark as an image and reports whether it did:
// only on a terminal with an image protocol, outside tmux, that
// answers what its background is.
func logoImage() bool {
  proto := graphics()
  if proto == protoNone {
    return false
  }

  light, ok := termBackground()
  if !ok {
    return false
  }

  png := logoOnDark
  if light {
    png = logoOnLight
  }

  // make room for the rows first, then draw at their top without
  // moving the cursor, and step past them: the header starts on the
  // same line whether the image showed or not
  rows := len(logoGrid) / 2
  fmt.Print(strings.Repeat("\n", rows) + fmt.Sprintf("\x1b[%dA\x1b7", rows))
  if proto == protoKitty {
    fmt.Print(kittyImage(png, rows))
  } else {
    fmt.Print(itermImage(png, rows))
  }

  fmt.Printf("\x1b8\x1b[%dB\r", rows)

  return true
}

// graphics says which image protocol the terminal speaks, from its
// environment: none under tmux, which drops these escapes.
func graphics() int {
  if os.Getenv("TMUX") != "" {
    return protoNone
  }

  term := os.Getenv("TERM")
  program := os.Getenv("TERM_PROGRAM")
  switch {
  case strings.Contains(term, "kitty"), strings.Contains(term, "ghostty"),
    program == "ghostty", program == "WezTerm",
    os.Getenv("KITTY_WINDOW_ID") != "":
    return protoKitty
  case program == "iTerm.app":
    return protoITerm
  }

  return protoNone
}

// kittyImage is png as kitty escapes: shown at once (a=T), rows tall
// with the width from its own proportions (r), the cursor left where
// it is (C=1), the terminal's replies off (q=2), and the base64 in
// chunks, m=1 on all but the last.
func kittyImage(png []byte, rows int) string {
  enc := base64.StdEncoding.EncodeToString(png)

  var b strings.Builder
  for start := 0; start < len(enc); start += kittyChunk {
    end := min(start + kittyChunk, len(enc))
    more := 1
    if end == len(enc) {
      more = 0
    }

    b.WriteString("\x1b_G")
    if start == 0 {
      fmt.Fprintf(&b, "a=T,f=100,r=%d,C=1,q=2,", rows)
    }

    fmt.Fprintf(&b, "m=%d;%s\x1b\\", more, enc[start:end])
  }

  return b.String()
}

// itermImage is png as iTerm2's inline image escape, rows tall, its
// proportions kept.
func itermImage(png []byte, rows int) string {
  return fmt.Sprintf(
    "\x1b]1337;File=inline=1;size=%d;height=%d;preserveAspectRatio=1:%s\a",
    len(png), rows, base64.StdEncoding.EncodeToString(png),
  )
}

// bgReply is the terminal's answer to OSC 11: its background color,
// 1 to 4 hex digits a channel.
var bgReply = regexp.MustCompile(
  `\]11;rgb:([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})/([0-9a-fA-F]{1,4})`,
)

// daReply is the answer to the device-attributes request sent after
// the color query: every terminal gives one, so it marks the end.
var daReply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)

// parseBackground reads a terminal's replies: whether its background
// is light, and whether it said at all.
func parseBackground(reply []byte) (bool, bool) {
  m := bgReply.FindSubmatch(reply)
  if m == nil {
    return false, false
  }

  var lum float64
  for i, weight := range []float64{ 0.299, 0.587, 0.114 } {
    v, _ := strconv.ParseUint(string(m[i + 1]), 16, 16)
    max := float64(uint64(1) << (4 * len(m[i + 1])) - 1)
    lum += weight * float64(v) / max
  }

  return lum > 0.5, true
}
