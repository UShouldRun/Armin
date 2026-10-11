// Package ui is a small, hand-rolled terminal styling layer — no
// external TUI framework, just ANSI codes, in keeping with the rest
// of this codebase's build-it-yourself style. Aims for the clean,
// fast, colored-but-not-noisy feel of tools like uv: a styled
// prompt, dim status lines, a spinner while the model thinks,
// nothing heavier.
package ui

import (
  "fmt"
  "os"
  "strings"
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
func Dim(s string) string     { return style(dimCode, s) }
func Red(s string) string     { return style(fgRed, s) }
func Green(s string) string   { return style(fgGreen, s) }
func Yellow(s string) string  { return style(fgYellow, s) }
func Blue(s string) string    { return style(fgBlue, s) }
func Magenta(s string) string { return style(fgMagenta, s) }
func Cyan(s string) string    { return style(fgCyan, s) }
func Gray(s string) string    { return style(fgGray, s) }

// Prompt is the input-line marker, styled like modern CLI tools — a
// bold colored chevron, not the classic "> ".
func Prompt() string {
  return Bold(Cyan("❯")) + " "
}

// Banner prints the startup summary line: binary name, project
// root, active model — the orientation a "uv"-style tool gives you
// up front instead of silence.
func Banner(root, model, baseURL string) {
  fmt.Println(Bold(Cyan("armin")) + Dim(" — terminal codebase assistant"))
  fmt.Printf("  %s %s\n", Dim("root:"), root)
  fmt.Printf(
    "  %s %s %s\n", Dim("model:"), model, Dim("(" + baseURL + ")"),
  )
  fmt.Println(Dim(
    "  type " + Bold("/help") + Dim(" for commands, ") +
      Bold("/quit") + Dim(" to exit"),
  ))
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
    fmt.Println("  " + Red("⎿ " + summary))
    return
  }

  fmt.Println(Dim("  ⎿ " + summary))
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

// Info prints a dim, low-priority status line — session save
// confirmations, auto-context notices, that kind of thing.
func Info(format string, args ...any) {
  fmt.Println(Dim(fmt.Sprintf(format, args...)))
}
