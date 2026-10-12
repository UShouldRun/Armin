package convo

import (
  "context"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"

  "armin/internal/llm"
)

func TestAnalyzeEmpty(t *testing.T) {
  s := Analyze(nil)
  if s != (Stats{}) {
    t.Fatalf("Analyze(nil) = %+v, want the zero value", s)
  }
}

func TestAnalyzeCountsMessagesToolCallsAndResults(t *testing.T) {
  history := []llm.Message{
    {Role: "system", Content: "0123456789"}, // 10 chars -> 2 tokens @4 chars/token
    {Role: "user", Content: "hello"},
    {
      Role:      "assistant",
      Content:   "",
      ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "search_code"}}},
    },
    {Role: "tool", ToolName: "search_code", Content: "result"},
    {Role: "assistant", Content: "done"},
  }

  s := Analyze(history)

  if s.Messages != 5 {
    t.Errorf("Messages = %d, want 5", s.Messages)
  }
  if s.ToolCalls != 1 {
    t.Errorf("ToolCalls = %d, want 1", s.ToolCalls)
  }
  if s.ToolResults != 1 {
    t.Errorf("ToolResults = %d, want 1", s.ToolResults)
  }
  if s.ApproxTokens <= 0 {
    t.Errorf("ApproxTokens = %d, want > 0", s.ApproxTokens)
  }
}

func TestCompactRefusesWhenTooShort(t *testing.T) {
  history := []llm.Message{
    {Role: "system", Content: "sys"},
    {Role: "user", Content: "hi"},
  }

  err := Compact(nil, nil, &history, 6, "")
  if err == nil {
    t.Fatal("expected an error when there's nothing meaningful to compact")
  }
  if len(history) != 2 {
    t.Fatal("Compact must not mutate history when it refuses to run")
  }
}

// fakeChatServer serves a canned /v1/chat/completions response so
// Compact's message-splicing logic can be tested without a real
// llama-server instance.
func fakeChatServer(t *testing.T, reply string) *httptest.Server {
  t.Helper()
  return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    resp := map[string]any{
      "choices": []map[string]any{
        { "message": map[string]string{ "role": "assistant", "content": reply } },
      },
    }
    err := json.NewEncoder(w).Encode(resp)
    if err != nil {
      t.Fatal(err)
    }
  }))
}

func TestCompactPreservesSystemAndTail(t *testing.T) {
  server := fakeChatServer(t, "- did X\n- next: Y")
  defer server.Close()
  client := llm.New(server.URL, "fake-model")

  history := []llm.Message{
    {Role: "system", Content: "you are armin"},
    {Role: "user", Content: "old message 1"},
    {Role: "assistant", Content: "old reply 1"},
    {Role: "user", Content: "old message 2"},
    {Role: "assistant", Content: "old reply 2"},
    {Role: "user", Content: "recent 1"},
    {Role: "assistant", Content: "recent 2"},
  }
  const keepLast = 2

  err := Compact(context.Background(), client, &history, keepLast, "")
  if err != nil {
    t.Fatalf("Compact: %v", err)
  }

  if history[0].Role != "system" || history[0].Content != "you are armin" {
    t.Fatalf("history[0] = %+v, want the original system prompt preserved", history[0])
  }
  if !strings.Contains(history[1].Content, "did X") {
    t.Fatalf("history[1] = %+v, want it to contain the model's summary", history[1])
  }

  tail := history[len(history) - keepLast:]
  if tail[0].Content != "recent 1" || tail[1].Content != "recent 2" {
    t.Fatalf("tail = %+v, want the last %d messages preserved verbatim", tail, keepLast)
  }

  if len(history) != 2 + keepLast {
    t.Fatalf("history has %d messages after compaction, want %d (system + summary + tail)", len(history), 2 + keepLast)
  }
}

func TestCompactNeverSplitsToolCallFromResult(t *testing.T) {
  server := fakeChatServer(t, "summary")
  defer server.Close()
  client := llm.New(server.URL, "fake-model")

  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "fix the bug" },
    { Role: "assistant", ToolCalls: []llm.ToolCall{ { ID: "a" }, { ID: "b" } } },
    { Role: "tool", ToolCallID: "a", Content: "r1" },
    { Role: "tool", ToolCallID: "b", Content: "r2" },
    { Role: "assistant", Content: "done" },
  }

  // keepLast 3 would start the tail at the second tool result.
  err := Compact(context.Background(), client, &history, 3, "scratch: plan")
  if err != nil {
    t.Fatalf("Compact: %v", err)
  }

  if history[1].Role != "user" {
    t.Fatalf("summary role = %q, want user (no mid-history system message)", history[1].Role)
  }
  if history[2].Role != "assistant" || len(history[2].ToolCalls) != 2 {
    t.Fatalf("tail starts with %+v, want the assistant tool call", history[2])
  }
  summary := history[1].Content
  for _, want := range []string{ "summary", "fix the bug", "scratch: plan" } {
    if !strings.Contains(summary, want) {
      t.Errorf("summary %q is missing %q", summary, want)
    }
  }
}

func TestSuggestParsesOneLinePerSuggestion(t *testing.T) {
  server := fakeChatServer(t, "What does search_code wrap?\nHow is dry-run handled?\nWhat's the sandbox timeout?")
  defer server.Close()
  client := llm.New(server.URL, "fake-model")

  history := []llm.Message{{Role: "user", Content: "hi"}}
  got, err := Suggest(context.Background(), client, history)
  if err != nil {
    t.Fatalf("Suggest: %v", err)
  }
  if len(got) != 3 {
    t.Fatalf("Suggest returned %d suggestions, want 3: %v", len(got), got)
  }
  if got[0] != "What does search_code wrap?" {
    t.Fatalf("got[0] = %q", got[0])
  }
}

func TestSuggestCapsAtMax(t *testing.T) {
  server := fakeChatServer(t, "one\ntwo\nthree\nfour\nfive")
  defer server.Close()
  client := llm.New(server.URL, "fake-model")

  got, err := Suggest(context.Background(), client, nil)
  if err != nil {
    t.Fatalf("Suggest: %v", err)
  }
  if len(got) != maxSuggestions {
    t.Fatalf("got %d suggestions, want the cap of %d", len(got), maxSuggestions)
  }
}

func TestSuggestSkipsBlankLines(t *testing.T) {
  server := fakeChatServer(t, "one\n\n\ntwo\n")
  defer server.Close()
  client := llm.New(server.URL, "fake-model")

  got, err := Suggest(context.Background(), client, nil)
  if err != nil {
    t.Fatalf("Suggest: %v", err)
  }
  if len(got) != 2 || got[0] != "one" || got[1] != "two" {
    t.Fatalf("got = %v, want [one two]", got)
  }
}

func TestStripListMarker(t *testing.T) {
  cases := map[string]string{
    "1. What does this do?":  "What does this do?",
    "2) Another question":    "Another question",
    "- bullet point":         "bullet point",
    "* star bullet":          "star bullet",
    "plain line":             "plain line",
    "3D rendering pipeline?": "3D rendering pipeline?",
    "":                       "",
  }
  for input, want := range cases {
    got := stripListMarker(input)
    if got != want {
      t.Errorf("stripListMarker(%q) = %q, want %q", input, got, want)
    }
  }
}

func TestTitleUsesModelReplyOrFallsBack(t *testing.T) {
  server := fakeChatServer(t, "\"Fix off-by-one in Mean.\"\nextra line")
  defer server.Close()
  client := llm.New(server.URL, "fake")
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "the test fails, find the bug please and fix it now" },
  }

  got := Title(context.Background(), client, history)
  if got != "Fix off-by-one in Mean" {
    t.Fatalf("Title = %q", got)
  }

  empty := fakeChatServer(t, "")
  defer empty.Close()
  got = Title(context.Background(), llm.New(empty.URL, "fake"), history)
  if got != "the test fails, find the bug" {
    t.Fatalf("fallback = %q", got)
  }
}
