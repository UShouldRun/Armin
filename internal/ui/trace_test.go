package ui

import (
  "errors"
  "fmt"
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

  wantTrace := "RunCommand:\n\t\n\t... in CheckExec:\n\t\"ps\" isn't in run_program's allowlist"
  if trace != wantTrace {
    t.Fatalf("trace = %q\nwant  %q", trace, wantTrace)
  }

  // no chain: all message
  m2, tr2 := SplitTrace("plain failure")
  if m2 != "plain failure" || tr2 != "" {
    t.Fatalf("plain: %q %q", m2, tr2)
  }

  // single-level chain
  m3, _ := SplitTrace("\n\t... in Load:\n\tbad config")
  if m3 != "bad config" {
    t.Fatalf("single = %q", m3)
  }
}

func TestJsonErrorMessage(t *testing.T) {
  if got := JsonErrorMessage(`llama-server returned 500: {"error":{"message":"slot lost the model","code":500}}`); got != "llama-server returned 500: slot lost the model" {
    t.Fatalf("with prefix = %q", got)
  }

  if got := JsonErrorMessage(`{"error":{"message":"slot lost the model","code":500}}`); got != "slot lost the model" {
    t.Fatalf("nested = %q", got)
  }

  if got := JsonErrorMessage(`{"message":"Regional routing requires a Business plan."}`); got != "Regional routing requires a Business plan." {
    t.Fatalf("flat = %q", got)
  }

  if got := JsonErrorMessage(`{"status":"error"}`); got != "" {
    t.Fatalf("no message field = %q", got)
  }

  if got := JsonErrorMessage("plain text body"); got != "" {
    t.Fatalf("non-JSON = %q", got)
  }

  // truncated JSON (the body was cut off): nothing to show, fall back
  if got := JsonErrorMessage(`llama-server returned 500: {"error":{"mes`); got != "" {
    t.Fatalf("truncated = %q", got)
  }
}

func TestErrorOf(t *testing.T) {
  err := fmt.Errorf("\n\t... in Complete:\n\t llama-server returned 500: {\"error\":{\"message\":\"slot lost the model\"}}")

  got := ErrorOf(err)
  if LastTrace == "" {
    t.Fatal("trace wasn't stashed")
  }

  if !contains(got, "slot lost the model") {
    t.Fatalf("ErrorOf = %q, want the JSON's message", got)
  }

  if contains(got, `{"error"`) {
    t.Fatalf("ErrorOf = %q, want the raw JSON hidden", got)
  }

  if !contains(got, "/trace") {
    t.Fatalf("ErrorOf = %q, want the expand hint", got)
  }

  // no chain: plain message, nothing stashed
  LastTrace = ""
  got = ErrorOf(errors.New("simple"))
  if got != "simple" || LastTrace != "" {
    t.Fatalf("plain: %q %q", got, LastTrace)
  }
}

func contains(s, sub string) bool {
  return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
    func() bool { for i := 0; i+len(sub) <= len(s); i++ { if s[i:i+len(sub)] == sub { return true } }; return false }())
}
