// Error display: the "... in Func" chain wrapped around every error is
// a trace for the developer, not the user. SplitTrace separates the
// two: the message keeps each frame's own words ("model call failed:
// calling llama-server at …: connection refused") without the
// function names; JSONErrorMessage pulls the human message out of the
// JSON bodies llama-server and OpenRouter send back on failure. The
// last trace is kept for /trace (ShowTrace).

package ui

import (
  "encoding/json"
  "fmt"
  "os"
  "strings"
  "sync/atomic"
)

// traceMarker opens every frame: each wrapper writes
// fmt.Errorf("\n\t... in Func:\n\t<its words>%w", err).
const traceMarker = "\n\t... in "

// lastTrace is the most recent error's full chain, for /trace. Errors
// are shown from several goroutines (the REPL, relayed frontends).
var lastTrace atomic.Pointer[string]

// traceFrames splits a chain into its frames, outermost first, and the
// text before the first marker (an unwrapped prefix, usually empty).
func traceFrames(s string) (string, []traceFrame) {
  parts := strings.Split(s, traceMarker)
  out := make([]traceFrame, 0, len(parts) - 1)
  for _, p := range parts[1:] {
    fn, text, _ := strings.Cut(p, ":\n\t")
    out = append(out, traceFrame{ fn: fn, text: strings.TrimSpace(text) })
  }

  return strings.TrimSpace(parts[0]), out
}

// SplitTrace splits an error string into the message to show and the
// chain to hide. The message joins every frame's own words, outermost
// first; an error with no chain is all message.
func SplitTrace(s string) (message, trace string) {
  if !strings.Contains(s, traceMarker) {
    return s, ""
  }

  prefix, fs := traceFrames(s)
  words := make([]string, 0, len(fs) + 1)
  if prefix != "" {
    words = append(words, prefix)
  }

  for _, f := range fs {
    if f.text != "" {
      words = append(words, f.text)
    }
  }

  return strings.Join(words, " "), strings.TrimPrefix(s, traceMarker)
}

// JSONErrorMessage pulls the message out of a JSON error body the text
// carries: llama-server's and OpenRouter's failures come back as
// {"error":{"message":...}} after a status prefix ("llama-server
// returned 500: ..."). It returns the prefix plus the message field,
// or "" when there's no such body.
func JSONErrorMessage(body string) string {
  i := strings.Index(body, `{"error"`)
  if i < 0 {
    i = strings.Index(body, `{"message"`)
  }

  if i < 0 {
    return ""
  }

  var parsed struct {
    Error struct {
      Message string `json:"message"`
    } `json:"error"`
    Message string `json:"message"`
  }

  err := json.Unmarshal([]byte(body[i:]), &parsed)
  if err != nil {
    return ""
  }

  msg := parsed.Error.Message
  if msg == "" {
    msg = parsed.Message
  }

  if msg == "" {
    return ""
  }

  prefix := strings.TrimSpace(body[:i])
  if prefix == "" {
    return msg
  }

  return prefix + " " + msg
}

// Readable is an error's text the way a person should read it: its
// message, with a JSON error body reduced to its message field.
func Readable(s string) (message, trace string) {
  message, trace = SplitTrace(s)
  readable := JSONErrorMessage(message)
  if readable != "" {
    message = readable
  }

  return message, trace
}

// Err prints err collapsed to its message, keeping the chain for
// /trace. The replacement for ui.Error("%s", err).
func Err(err error) {
  Error("%s", ErrorOf(err))
}

// ErrText is Err for a string that's already an error's text: shared
// frontends receive errors as strings (share.Event.Delta).
func ErrText(s string) {
  Error("%s", ErrorText(s))
}

// ErrorOf renders err for the terminal: the message, and a hint when
// there's a chain to expand.
func ErrorOf(err error) string {
  return ErrorText(err.Error())
}

// ErrorText is ErrorOf for an error already rendered to a string.
func ErrorText(s string) string {
  message, trace := Readable(s)
  if trace == "" {
    lastTrace.Store(nil)
    return message
  }

  lastTrace.Store(&s)

  return message + "  " + Dim("(/trace to expand)")
}

// FormatTrace lays a chain out one frame per line, outermost first:
// "  ... in Func: its words".
func FormatTrace(s string) string {
  prefix, fs := traceFrames(s)
  var b strings.Builder
  if prefix != "" {
    b.WriteString("  " + prefix + "\n")
  }

  for _, f := range fs {
    b.WriteString("  ... in " + f.fn)
    if f.text != "" {
      b.WriteString(": " + strings.ReplaceAll(f.text, "\n\t", "\n    "))
    }

    b.WriteByte('\n')
  }

  return b.String()
}

// ShowTrace prints the last error's chain.
func ShowTrace() {
  s := lastTrace.Load()
  if s == nil {
    Info("no error to expand yet")
    return
  }

  fmt.Fprint(os.Stderr, Red("trace:") + "\n" + Dim(FormatTrace(*s)))
}
