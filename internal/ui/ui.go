// Package ui is a small, hand-rolled terminal styling layer — no
// external TUI framework, just ANSI codes, in keeping with the rest
// of this codebase's build-it-yourself style. Aims for the clean,
// fast, colored-but-not-noisy feel of tools like uv: a styled
// prompt, dim status lines, a spinner while the model thinks,
// nothing heavier.
//
//   ui.go          colors and styles, the prompt, the tool line
//   wrap.go        word wrapping to the terminal's width
//   markdown.go    Markdown rendered for the terminal, streamed or
//                  whole (tables kept whole, code blocks highlighted)
//   spinner.go     the spinner and the status line beside it
//   lineinput.go   the line editor: ghost-text suggestions, pastes
//                  folded into labels, the key pump
//   keys.go        decoding keys and bracketed pastes from raw input
//   composer.go    the input bar under a running turn
//   trace.go       error messages with their trace folded away
//   rawmode*.go,   the terminal in raw mode, and its size
//   termsize_*.go
package ui

import (
  _ "embed"
  "encoding/base64"
  "fmt"
  "os"
  "strings"

  "armin/internal/build"
)

const (
  reset   = "\x1b[0m"
  bold    = "\x1b[1m"
  dimCode = "\x1b[2m"

  fgRed     = "\x1b[31m"
  fgGreen   = "\x1b[32m"
  fgYellow  = "\x1b[33m"
  fgBlue    = "\x1b[34m"
  fgMagenta = "\x1b[35m"
  fgCyan    = "\x1b[36m"
  fgGray    = "\x1b[90m"

  // strongCode is **bold** in an answer: bold in everblush's yellow,
  // the same color the web UI gives it
  strongCode = "\x1b[1;38;2;229;199;107m"
)

// Enabled controls whether style codes are emitted at all. Computed
// once at startup from terminal-ness and NO_COLOR, but left mutable
// so --no-color can force it off.
var Enabled = isTTY(os.Stdout) && os.Getenv("NO_COLOR") == ""

func isTTY(f *os.File) bool {
  info, err := f.Stat()
  if err != nil {
    return false
  }

  return info.Mode() & os.ModeCharDevice != 0
}

func style(code, s string) string {
  if !Enabled {
    return s
  }

  return code + s + reset
}

func Bold(s string) string    { return style(bold, s) }

// Strong is an answer's **bold**: charged, not just heavier.
func Strong(s string) string { return style(strongCode, s) }

func Dim(s string) string     { return style(dimCode, s) }
func Red(s string) string     { return style(fgRed, s) }
func Green(s string) string   { return style(fgGreen, s) }
func Yellow(s string) string  { return style(fgYellow, s) }
func Magenta(s string) string { return style(fgMagenta, s) }
func Cyan(s string) string    { return style(fgCyan, s) }

// Italic, Underline and Strike are for rendered markdown.
func Italic(s string) string    { return style("\x1b[3m", s) }
func Underline(s string) string { return style("\x1b[4m", s) }
func Strike(s string) string    { return style("\x1b[9m", s) }

// Prompt is the input-line marker, styled like modern CLI tools — a
// bold colored chevron, not the classic "> ".
func Prompt() string {
  return Bold(Cyan("❯")) + " "
}

// UserLine is a message the user sent, after the prompt: its later
// lines go under its first, so it reads as one message.
func UserLine(text string) string {
  return Prompt() + strings.ReplaceAll(text, "\n", "\n  ")
}

// logoPNG is the banner's mark, built into the binary and shown as a
// real image when the terminal can display one (kitty, ghostty and
// iTerm2's protocols): the dark logo, its cream blades on #141414.
// A var so tests can swap in their own PNG.
//
// The file must stay small enough for its base64 to fit one 4096-byte
// kitty chunk: older ghostty (1.3) drops the image silently on
// multi-chunk transfers, and 1264x848 was ~12 chunks.
//
//go:embed logo.png
var logoPNG []byte

// LogoCols is the mark's width in terminal columns, so the header
// text starts beside it instead of under it. Half the PNG's pixels,
// with a cell about twice as tall as wide.
var LogoCols = 18

// LogoRows is the mark's height in terminal rows.
var LogoRows = 3

// bannerText prints the header's text column: the name and its
// tagline, then the project root. The model already shows in the
// instance line below, so it doesn't get one of its own.
func bannerText(root, model, baseURL string) {
  fmt.Println(Bold(Cyan(build.Name)) + Dim(" — terminal codebase assistant"))
  fmt.Printf("  %s %s\n\n", Dim("root:"), root)
}

// Logo prints the mark inline when the terminal speaks a graphics
// protocol (kitty's APC or iTerm2's OSC 1337), guessed from TERM and
// TERM_PROGRAM. Terminals that don't get nothing: the header stays
// readable, the logo stays optional.
func Logo() {
  if !Enabled {
    return
  }

  // tmux doesn't pass kitty's APC image escapes through: the base64
  // lands as literal text on the screen. Skip the logo there rather
  // than garble the header.
  if os.Getenv("TMUX") != "" {
    return
  }

  program := os.Getenv("TERM_PROGRAM")
  term := os.Getenv("TERM")
  kitty := strings.Contains(term, "kitty") ||
    strings.Contains(term, "ghostty") || program == "ghostty"
  iterm := program == "iterm.app" || program == "iterm2"

  if !kitty && !iterm {
    return
  }

  data := logoPNG

  if iterm {
    fmt.Printf(
      "\x1b]1337;File=inline=1;width=%dpx;height=%dpx:%s\x1b\\",
      LogoCols * 8, LogoRows * 16,
      base64.StdEncoding.EncodeToString(data),
    )
    fmt.Println()

    return
  }

  // kitty: the image streams as base64 chunks inside APC escapes —
  // m=1 on every chunk but the last, m=0 on that one, 4096 payload
  // bytes at most. Every chunk is a whole escape: the first carries
  // the image params, continuations carry only m.
  enc := base64.StdEncoding.EncodeToString(data)
  chunks := (len(enc) + 4095) / 4096
  for i := 0; i < chunks; i++ {
    start := i * 4096
    end := start + 4096
    if end > len(enc) {
      end = len(enc)
    }

    // a=T transmits and displays: kitty's default, a=t, only stores
    // the image, and nothing shows
    params := ""
    if i == 0 {
      params = "a=T,f=100,c=" + fmt.Sprint(LogoCols) +
        ",r=" + fmt.Sprint(LogoRows) + ","
    }

    m := "m=1;"
    if i == chunks - 1 {
      m = "m=0;"
    }

    fmt.Print("\x1b_G" + params + m + enc[start:end] + "\x1b\\")
  }

  // the cursor ends beside the image's last row: the header starts
  // on the line under it
  fmt.Println()
}

// Banner prints the startup summary: the logo (a real image on
// terminals that show one) above the name and tagline, then project
// root and active model. Notices from background work land under it
// (REPL.Notice), keeping the header on top.
func Banner(root, model, baseURL string) {
  Logo()
  bannerText(root, model, baseURL)
}

// Instance names the armin instance a frontend shows, so a terminal
// and nvim sharing one say so alike: "Armin local · instance 1".
func Instance(model string, n int, note string) {
  line := "Armin " + model
  if n > 0 {
    line += " · instance " + fmt.Sprint(n)
  }

  fmt.Printf("  %s %s\n", Bold(line), Dim(note))
}

// ClearLine wipes the current terminal line, so what prints next
// starts at column 0. A no-op without a terminal (piped, -no-color).
func ClearLine() {
  if Enabled {
    fmt.Print("\r\x1b[K")
  }
}

// ToolLine prints a tool call as the model makes it: a bullet, the
// tool's name in bold and its arguments in full (see
// agent.CallLine), like Claude Code shows its own.
func ToolLine(line string) {
  name, rest, _ := strings.Cut(line, "  ")
  fmt.Println(Cyan("● ") + Bold(name) + "  " + rest)
}

// ToolResultLine prints what a tool returned, one line under its
// call; errors in red.
func ToolResultLine(summary string) {
  if strings.HasPrefix(summary, "error:") {
    fmt.Println("  " + Red("└ " + summary))
    return
  }

  fmt.Println(Dim("  └ " + summary))
}

// ToolCall renders a one-line, dim "doing X" announcement just
// before a tool executes — the same kind of live transparency
// Claude Code gives you, so a slow local model doesn't feel hung.
func ToolCall(name string, args map[string]any) {
  fmt.Println(Dim("  → " + name + formatArgs(args)))
}

func formatArgs(args map[string]any) string {
  if len(args) == 0 {
    return "()"
  }

  const maxValue = 60

  parts := make([]string, 0, len(args))
  for k, v := range args {
    s := strings.ReplaceAll(fmt.Sprint(v), "\n", "⏎")
    if len(s) > maxValue {
      s = fmt.Sprintf("%s… (%d bytes)", s[:maxValue], len(s))
    }

    parts = append(parts, k + "=" + s)
  }

  return "(" + strings.Join(parts, ", ") + ")"
}

// Error prints a user-facing error line, consistently styled.
func Error(format string, args ...any) {
  fmt.Fprintln(os.Stderr, Red("error: ") + fmt.Sprintf(format, args...))
}

// Warn prints something the user should act on, on stderr: -lsp
// speaks its protocol on stdout.
func Warn(format string, args ...any) {
  fmt.Fprintln(os.Stderr, Yellow("warning: ") + fmt.Sprintf(format, args...))
}

// Info prints a dim, low-priority status line — session save
// confirmations, auto-context notices, that kind of thing.
func Info(format string, args ...any) {
  fmt.Println(Dim(fmt.Sprintf(format, args...)))
}
