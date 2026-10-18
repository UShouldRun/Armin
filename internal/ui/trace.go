// Error display: the ... in Func chain wrapped around every error is
// a trace for the developer, not the user. SplitTrace separates the
// two, and JsonErrorMessage pulls the human message out of the JSON
// bodies llama-server and OpenRouter send back on failure, so the
// user sees "slot lost the model" instead of the JSON that said it.

package ui

import (
  "encoding/json"
  "fmt"
  "os"
  "strings"
)

// SplitTrace splits an error string into the message to show and the
// "... in Func" chain to hide. Each wrapper adds
// "\n\t... in Func:\n\t" around what it wraps, so the innermost
// message sits after the LAST part's "\n\t" — and since the wrapping
// is applied outermost-last, the last marker in the string is the
// innermost frame's name, with the original message after its
// newline. An error with no chain is all message.
//
//   message, trace := SplitTrace(err.Error())
//   ui.Error("%s", message)        // what the user reads
//   if trace != "" { ... }         // "/trace" or an expand control
//
func SplitTrace(s string) (message, trace string) {
  const marker = "\n\t... in "

  if !strings.Contains(s, marker) {
    return s, ""
  }

  parts := strings.Split(s, marker)

  // The final part is "Func:\n\t<message>": the message is what
  // follows that part's first newline indent (a multi-line leaf
  // message keeps its remaining lines), the rest is the trace.
  lastPart := parts[len(parts)-1]
  _, after, found := strings.Cut(lastPart, "\n\t")
  if !found {
    // a marker with nothing after it: degenerate, show it all
    return s, strings.TrimPrefix(s, marker)
  }

  return after, strings.TrimPrefix(s, marker)
}

// JsonErrorMessage pulls the message out of a JSON error body the
// message carries — llama-server's and OpenRouter's failures come
// back as {"error":{"message":...}} pasted after a status prefix
// ("llama-server returned 500: ..."). It returns the prefix plus the
// message field, so the readable parts stay and the JSON goes; ""
// when there's no such body. The raw form stays in the trace.
func JsonErrorMessage(body string) string {
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
  sep := " "
  if prefix == "" {
    sep = ""
  }

  return prefix + sep + msg
}

// LastTrace is the most recent error's full chain, stashed by ErrorOf
// for /trace to print. Frontends without the command (the web UI)
// never read it.
var LastTrace string

// Err prints err collapsed to its message, stashing the full chain
// in LastTrace for /trace. The replacement for ui.Error("%s", err)
// at every error call site.
func Err(err error) {
  Error("%s", ErrorOf(err))
}

// ErrText is Err for a string that's already an error's text —
// shared frontends receive errors as strings (share.Event.Delta).
func ErrText(s string) {
  Error("%s", ErrorText(s))
}

// ErrorOf renders err the way the frontends want it: the message to
// read, with the trace stashed in LastTrace for whoever expands it.
func ErrorOf(err error) string {
  return ErrorText(err.Error())
}

// ErrorText is ErrorOf for an error already rendered to a string.
func ErrorText(s string) string {
  message, trace := SplitTrace(s)
  if trace == "" {
    LastTrace = ""
    return message
  }

  LastTrace = s

  // a JSON error body in the message: show its message field, the
  // raw JSON stays in the trace
  if j := JsonErrorMessage(message); j != "" {
    message = j
  }

  return message + "  " + Dim("(/trace to expand)")
}

// ShowTrace prints the stashed chain, innermost frame last.
func ShowTrace() {
  if LastTrace == "" {
    Info("no error to expand yet")
    return
  }

  fmt.Fprintln(os.Stderr, Red("trace: ") + Dim(strings.ReplaceAll(
    "\n\t... in " + LastTrace, "\n\t", "\n    "),
  ))
}
