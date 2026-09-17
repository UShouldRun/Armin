package agent

import (
  "context"
  "encoding/json"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "testing"

  "armin/internal/llm"
  "armin/internal/sandbox"
)

func TestToolDefinitionsShape(t *testing.T) {
  defs := ToolDefinitions()
  if len(defs) != 4 {
    t.Fatalf("ToolDefinitions() has %d entries, want 4", len(defs))
  }

  names := map[string]bool{}
  for _, d := range defs {
    if d.Type != "function" {
      t.Errorf("tool %q has Type %q, want \"function\"", d.Function.Name, d.Type)
    }
    if d.Function.Description == "" {
      t.Errorf("tool %q has an empty description", d.Function.Name)
    }
    names[d.Function.Name] = true
  }

  for _, want := range []string{"search_code", "read_file", "list_directory", "run_command"} {
    if !names[want] {
      t.Errorf("ToolDefinitions() missing %q", want)
    }
  }
}

func newTestLoop(t *testing.T, chatHandler http.HandlerFunc) (*Loop, string) {
  t.Helper()

  mux := http.NewServeMux()
  mux.HandleFunc("/api/chat", chatHandler)
  server := httptest.NewServer(mux)
  t.Cleanup(server.Close)

  root := t.TempDir()
  if err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello world"), 0o644); err != nil {
    t.Fatal(err)
  }

  sb, err := sandbox.New(root, sandbox.Options{})
  if err != nil {
    t.Fatal(err)
  }

  client := llm.New(server.URL, "fake-model")
  return New(client, sb), root
}

// TestTurnExecutesToolThenReturnsAnswer drives a full round trip: the
// fake model first asks for read_file, then (once it sees the tool
// result) answers in plain text — the same shape a real Ollama
// tool-calling exchange has.
func TestTurnExecutesToolThenReturnsAnswer(t *testing.T) {
  round := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    var reply llm.Message
    if round == 1 {
      reply = llm.Message{
        Role: "assistant",
        ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{
          Name:      "read_file",
          Arguments: map[string]any{"path": "f.txt"},
        }}},
      }
    } else {
      reply = llm.Message{Role: "assistant", Content: "the file says hello world"}
    }
    json.NewEncoder(w).Encode(map[string]any{"message": reply, "done": true})
  })

  var toolCallSeen string
  loop.OnToolCall = func(name string, _ map[string]any) { toolCallSeen = name }

  history := []llm.Message{{Role: "system", Content: "sys"}}
  answer, err := loop.Turn(context.Background(), &history, nil)
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }
  if answer != "the file says hello world" {
    t.Fatalf("answer = %q", answer)
  }
  if toolCallSeen != "read_file" {
    t.Fatalf("OnToolCall saw %q, want read_file", toolCallSeen)
  }
  if round != 2 {
    t.Fatalf("model was called %d times, want exactly 2 (tool call, then answer)", round)
  }

  // history should now hold: system, assistant(tool_calls), tool(result), assistant(answer)
  if len(history) != 4 {
    t.Fatalf("history has %d messages, want 4, got %+v", len(history), history)
  }
  if history[2].Role != "tool" || history[2].ToolName != "read_file" {
    t.Fatalf("history[2] = %+v, want the read_file tool result", history[2])
  }
}

func TestTurnSurfacesToolErrorAsStructuredResult(t *testing.T) {
  round := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    var reply llm.Message
    if round == 1 {
      reply = llm.Message{
        Role: "assistant",
        ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{
          Name:      "read_file",
          Arguments: map[string]any{"path": "does-not-exist.txt"},
        }}},
      }
    } else {
      reply = llm.Message{Role: "assistant", Content: "couldn't find that file"}
    }
    json.NewEncoder(w).Encode(map[string]any{"message": reply, "done": true})
  })

  history := []llm.Message{{Role: "system", Content: "sys"}}
  if _, err := loop.Turn(context.Background(), &history, nil); err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if history[2].Role != "tool" {
    t.Fatalf("history[2] = %+v, want a tool-role error result", history[2])
  }
  var result map[string]string
  if err := json.Unmarshal([]byte(history[2].Content), &result); err != nil {
    t.Fatalf("tool error result isn't valid JSON: %v", err)
  }
  if result["error"] == "" {
    t.Fatal("expected the tool error result to carry a non-empty \"error\" field")
  }
}

func TestTurnHitsMaxRounds(t *testing.T) {
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    // Always asks for another tool call — a confused model that never
    // produces a final answer.
    reply := llm.Message{
      Role: "assistant",
      ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{
        Name:      "list_directory",
        Arguments: map[string]any{},
      }}},
    }
    json.NewEncoder(w).Encode(map[string]any{"message": reply, "done": true})
  })

  history := []llm.Message{{Role: "system", Content: "sys"}}
  _, err := loop.Turn(context.Background(), &history, nil)
  if err == nil {
    t.Fatal("expected an error once maxToolRounds is exceeded")
  }
}

func TestTurnRejectsUnknownTool(t *testing.T) {
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    reply := llm.Message{
      Role: "assistant",
      ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "delete_everything"}}},
    }
    json.NewEncoder(w).Encode(map[string]any{"message": reply, "done": true})
  })

  history := []llm.Message{{Role: "system", Content: "sys"}}
  // The fake server always offers the same bogus tool call, so this
  // will run out the clock on maxToolRounds — what matters here is
  // that execute() doesn't panic on an unrecognized tool name and
  // instead produces a structured error result each round.
  if _, err := loop.Turn(context.Background(), &history, nil); err == nil {
    t.Fatal("expected the max-rounds error")
  }
  if history[1].Role != "assistant" {
    t.Fatalf("history[1] = %+v", history[1])
  }
}
