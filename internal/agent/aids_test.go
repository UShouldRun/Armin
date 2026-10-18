package agent

import (
  "context"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"

  "armin/internal/llm"
)

func TestParseAids(t *testing.T) {
  a, ok := ParseAids("stuck, verify")
  if !ok || !a.Has(AidStuck) || !a.Has(AidVerify) || a.Has(AidProcedure) {
    t.Fatalf("ParseAids(stuck, verify) = %08b, %v", a, ok)
  }

  a, ok = ParseAids("all")
  if !ok || a != AidAll {
    t.Fatalf("all = %08b", a)
  }

  _, ok = ParseAids("stuck,magic")
  if ok {
    t.Fatal("an unknown aid parsed")
  }
}

func TestStall(t *testing.T) {
  var s stall
  fail := []byte(`{"passed":false,"output":"FAIL"}`)
  pass := []byte(`{"passed":true}`)
  missed := []byte(`{"error":"no match"}`)
  if s.observe(toolRunTests, fail) || s.observe(toolEditFile, missed) {
    t.Fatal("stepped back before three misses")
  }

  if !s.observe(toolRunProgram, []byte(`{"exit_code":2}`)) {
    t.Fatal("no step back after three misses")
  }

  // a passing check resets the count
  s.observe(toolRunTests, fail)
  s.observe(toolRunTests, pass)
  s.observe(toolRunTests, fail)
  if s.observe(toolRunTests, fail) {
    t.Fatal("the pass in between didn't reset the count")
  }

  // at most maxNudges a turn
  s.observe(toolRunTests, fail)
  for range 9 {
    if s.observe(toolRunTests, fail) && s.nudges > maxNudges {
      t.Fatal("more nudges than maxNudges")
    }
  }
}

func TestEffortLine(t *testing.T) {
  h := []llm.Message{ { Role: "system", Content: "You are Armin." } }
  setEffortLine(h, "high")
  if !strings.HasPrefix(h[0].Content, "Reasoning effort is set to xhigh.") ||
    !strings.HasSuffix(h[0].Content, "\n\nYou are Armin.") {
    t.Fatalf("high: %q", h[0].Content)
  }

  setEffortLine(h, "low")
  if !strings.HasPrefix(h[0].Content, "Reasoning effort is set to low.") ||
    strings.Contains(h[0].Content, "xhigh") {
    t.Fatalf("low after high: %q", h[0].Content)
  }

  setEffortLine(h, "medium")
  if h[0].Content != "You are Armin." {
    t.Fatalf("medium: %q", h[0].Content)
  }
}

func TestProcedure(t *testing.T) {
  l := &Loop{ Aids: AidProcedure }
  h := []llm.Message{ { Role: "user", Content: "the test fails" } }
  l.addProcedure(h, TaskFix)
  if !strings.Contains(h[0].Content, "How to approach this fix task") ||
    !strings.Contains(h[0].Content, "reproduce the failure") {
    t.Fatalf("procedure: %q", h[0].Content)
  }

  h[0].Content = "hi"
  (&Loop{}).addProcedure(h, TaskFix)
  l.addProcedure(h, TaskOther)
  if h[0].Content != "hi" {
    t.Fatalf("procedure added when off or for other: %q", h[0].Content)
  }
}

func TestVerify(t *testing.T) {
  var asked string
  reply := "ISSUE: the tests were never run after the edit."
  answer := func(w http.ResponseWriter, r *http.Request) {
    var req struct {
      Messages []struct{ Content string } `json:"messages"`
    }

    json.NewDecoder(r.Body).Decode(&req)
    asked = req.Messages[1].Content
    msg := map[string]any{ "role": "assistant", "content": reply }
    json.NewEncoder(w).Encode(map[string]any{
      "choices": []any{ map[string]any{ "message": msg } },
    })
  }

  server := httptest.NewServer(http.HandlerFunc(answer))
  defer server.Close()

  l := &Loop{ LLM: llm.New(server.URL, "local") }
  call := llm.ToolCall{ Function: llm.ToolCallFunction{
    Name: "edit_file", Arguments: map[string]any{ "path": "a.go" },
  } }
  h := []llm.Message{
    { Role: "user", Content: "fix the bug" },
    { Role: "assistant", ToolCalls: []llm.ToolCall{ call } },
    { Role: "tool", Content: `{"ok":true}` },
    { Role: "assistant", Content: "Fixed and tested." },
  }

  if !changedOrRan(h, 0) || changedOrRan(h[3:], 0) {
    t.Fatal("changedOrRan")
  }

  issue := l.verify(context.Background(), h, 0)
  if issue != "the tests were never run after the edit." {
    t.Fatalf("verify = %q", issue)
  }

  if !strings.Contains(asked, "fix the bug") ||
    !strings.Contains(asked, "edit_file") ||
    !strings.Contains(asked, "Fixed and tested.") {
    t.Fatalf("the check didn't see the turn:\n%s", asked)
  }

  reply = "OK"
  if l.verify(context.Background(), h, 0) != "" {
    t.Fatal("OK reported an issue")
  }
}
