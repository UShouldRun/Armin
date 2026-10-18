package ui

import (
  "errors"
  "fmt"
  "strings"
  "testing"
)

func TestSplitTrace(t *testing.T) {
  // the shape every wrapper in the codebase produces
  s := fmt.Errorf("\n\t... in RunCommand:\n\t%w",
    fmt.Errorf("\n\t... in CheckExec:\n\t%w",
      errors.New("\"ps\" isn't in run_program's allowlist"))).Error()

  message, trace := SplitTrace(s)
  if message != "\"ps\" isn't in run_program's allowlist" {
    t.Fatalf("message = %q", message)
  }

  want := "RunCommand:\n\t\n\t... in CheckExec:\n\t\"ps\" isn't in " +
    "run_program's allowlist"
  if trace != want {
    t.Fatalf("trace = %q\nwant  %q", trace, want)
  }

  // a middle frame's words stay in the message, without its name
  s = fmt.Errorf("\n\t... in Turn:\n\t model call failed: %w",
    fmt.Errorf("\n\t... in ChatStream:\n\t calling llama-server at "+
      "http://x: %w", errors.New("connection refused"))).Error()
  message, _ = SplitTrace(s)
  want = "model call failed: calling llama-server at http://x: " +
    "connection refused"
  if message != want {
    t.Fatalf("joined = %q\nwant     %q", message, want)
  }

  plain, none := SplitTrace("plain failure")
  if plain != "plain failure" || none != "" {
    t.Fatalf("plain: %q %q", plain, none)
  }

  single, _ := SplitTrace("\n\t... in Load:\n\tbad config")
  if single != "bad config" {
    t.Fatalf("single = %q", single)
  }
}

func TestJSONErrorMessage(t *testing.T) {
  cases := []struct{ in, want string }{
    { `llama-server returned 500: ` +
        `{"error":{"message":"slot lost the model","code":500}}`,
      "llama-server returned 500: slot lost the model" },
    { `{"error":{"message":"slot lost the model","code":500}}`,
      "slot lost the model" },
    { `{"message":"Regional routing requires a Business plan."}`,
      "Regional routing requires a Business plan." },
    { `{"status":"error"}`, "" },
    { "plain text body", "" },
    { `llama-server returned 500: {"error":{"mes`, "" }, // cut off
  }

  for _, c := range cases {
    got := JSONErrorMessage(c.in)
    if got != c.want {
      t.Fatalf("JSONErrorMessage(%q) = %q, want %q", c.in, got, c.want)
    }
  }
}

func TestErrorOfAndTrace(t *testing.T) {
  err := fmt.Errorf("\n\t... in Complete:\n\t llama-server returned 500: " +
    "{\"error\":{\"message\":\"slot lost the model\"}}")

  got := ErrorOf(err)
  if lastTrace.Load() == nil {
    t.Fatal("trace wasn't kept")
  }

  if !strings.Contains(got, "slot lost the model") ||
    strings.Contains(got, `{"error"`) || !strings.Contains(got, "/trace") {
    t.Fatalf("ErrorOf = %q", got)
  }

  // the trace reads one frame per line, no stray marker
  lines := FormatTrace(err.Error())
  first := "  ... in Complete: llama-server returned 500"
  if !strings.HasPrefix(lines, first) ||
    strings.Count(lines, "... in") != 1 {
    t.Fatalf("FormatTrace = %q", lines)
  }

  got = ErrorOf(errors.New("simple"))
  if got != "simple" || lastTrace.Load() != nil {
    t.Fatalf("plain: %q, trace kept: %v", got, lastTrace.Load() != nil)
  }
}
