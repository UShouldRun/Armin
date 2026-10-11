package agent

import (
  "context"
  "encoding/json"
  "net/http"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/llm"
  "armin/internal/notes"
)

func call(id, name string, args map[string]any) llm.ToolCall {
  return llm.ToolCall{ ID: id, Function: llm.ToolCallFunction{ Name: name, Arguments: args } }
}

func TestEditNeedsReadThenAsksApproval(t *testing.T) {
  round := 0
  loop, root := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    switch round {
    case 1:
      writeChatResponse(t, w, llm.Message{ Role: "assistant", ToolCalls: []llm.ToolCall{
        call("e1", "edit_file", map[string]any{ "path": "f.txt", "old_string": "hello", "new_string": "bye" }),
      } })
    case 2:
      writeChatResponse(t, w, llm.Message{ Role: "assistant", ToolCalls: []llm.ToolCall{
        call("r1", "read_file", map[string]any{ "path": "f.txt" }),
        call("e2", "edit_file", map[string]any{ "path": "f.txt", "old_string": "hello", "new_string": "bye" }),
      } })
    default:
      writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "done" })
    }
  })

  var previews []string
  hooks := Hooks{
    Approve: func(tool, preview string) (bool, string) {
      previews = append(previews, preview)
      return true, ""
    },
  }

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  if _, err := loop.Turn(context.Background(), &history, hooks); err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if !strings.Contains(history[2].Content, "hasn't been read") {
    t.Fatalf("first edit result = %s, want a read-first error", history[2].Content)
  }

  if len(previews) != 1 || !strings.Contains(previews[0], "+ bye world") {
    t.Fatalf("previews = %q, want one diff showing the change", previews)
  }

  got, _ := os.ReadFile(filepath.Join(root, "f.txt"))
  if string(got) != "bye world" {
    t.Fatalf("file = %q", got)
  }
}

func TestDeclinedEditCarriesFeedback(t *testing.T) {
  round := 0
  loop, root := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    if round == 1 {
      writeChatResponse(t, w, llm.Message{ Role: "assistant", ToolCalls: []llm.ToolCall{
        call("w1", "write_file", map[string]any{ "path": "new.txt", "content": "x" }),
      } })

      return
    }

    writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "ok" })
  })

  hooks := Hooks{
    Approve: func(string, string) (bool, string) { return false, "name it NEW.txt" },
  }

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  loop.Turn(context.Background(), &history, hooks)

  if !strings.Contains(history[2].Content, "name it NEW.txt") {
    t.Fatalf("result = %s, want the user's feedback", history[2].Content)
  }

  if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
    t.Fatal("a declined write reached disk")
  }
}

func TestReadOnlyDropsWriteTools(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  loop.ReadOnly = true
  for _, d := range loop.Definitions() {
    if toolIDs[d.Function.Name].isWrite() {
      t.Fatalf("read-only loop offers %s", d.Function.Name)
    }
  }
}

func TestNoteToolsOnlyWhenConfigured(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  base := len(loop.Definitions())

  loop2, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  loop2.Memories = &notes.Memories{ ProjectDir: t.TempDir(), UserDir: t.TempDir() }
  loop2.Scratch = &notes.Scratchpad{ Dir: t.TempDir() }
  if got := len(loop2.Definitions()); got != base + 6 {
    t.Fatalf("definitions = %d, want %d (+3 memory, +3 scratchpad)", got, base + 6)
  }

  out := loop2.execute(context.Background(), call("1", "scratchpad_write", map[string]any{
    "name": "plan", "content": "step 1",
  }), Hooks{})
  if f, ok := out.(notes.ScratchFile); !ok || f.Name != "plan" {
    t.Fatalf("scratchpad_write = %#v", out)
  }
}

func TestEndTurnOnlyStripsReasoning(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  big := strings.Repeat("x", 2000)
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "turn 1" },
    { Role: "tool", ToolName: "read_file", ToolCallID: "a", Content: big },
    { Role: "user", Content: "turn 2" },
    { Role: "user", Content: "turn 3" },
    { Role: "assistant", Content: "done", Reasoning: "long thoughts" },
  }

  loop.endTurn(&history, 5, Hooks{})

  if history[5].Reasoning != "" {
    t.Fatal("reasoning survived the end of its turn")
  }

  if history[2].Content != big {
    t.Fatal("endTurn trimmed history with no context pressure (breaks prompt caching)")
  }
}

func TestTrimUnderPressureKeepsRecentTurns(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  loop.KeepTurns = 1
  loop.ContextSize = 2000 // trims past ~1000 tokens, summarizes past 1500

  big := strings.Repeat("x", 2400)
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "turn 1" },
    { Role: "assistant", ToolCalls: []llm.ToolCall{
      call("a", "read_file", map[string]any{ "path": "big.go" }),
      call("b", "write_file", map[string]any{ "path": "n.go", "content": big }),
    } },
    { Role: "tool", ToolName: "read_file", ToolCallID: "a", Content: big },
    { Role: "tool", ToolName: "write_file", ToolCallID: "b", Content: `{"path":"n.go"}` },
    { Role: "user", Content: "turn 2" },
    { Role: "assistant", ToolCalls: []llm.ToolCall{ call("c", "read_file", map[string]any{ "path": "x.go" }) } },
    { Role: "tool", ToolName: "read_file", ToolCallID: "c", Content: big },
  }

  if !loop.manageContext(context.Background(), &history, llm.Usage{}, Hooks{}) {
    t.Fatal("expected a trim")
  }

  var stub map[string]any
  if err := json.Unmarshal([]byte(history[3].Content), &stub); err != nil {
    t.Fatalf("old result isn't a JSON stub: %q", history[3].Content)
  }

  if !strings.Contains(stub["elided"].(string), "path=big.go") {
    t.Fatalf("stub = %v, want it to name the call", stub)
  }

  if c := history[2].ToolCalls[1].Function.Arguments["content"].(string); len(c) > 100 {
    t.Fatal("old write_file content wasn't shrunk")
  }

  if history[7].Content != big {
    t.Fatal("the current turn's result was trimmed")
  }
}

func TestAutoCompactBeforeOverflow(t *testing.T) {
  summarized := false
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    var req struct {
      Tools    []any         `json:"tools"`
      Messages []llm.Message `json:"messages"`
    }

    json.NewDecoder(r.Body).Decode(&req)
    if len(req.Tools) == 0 {
      summarized = true
      writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "summary" })

      return
    }

    writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "answer" })
  })
  loop.ContextSize = 1000

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  for i := 0; i < 10; i++ {
    history = append(history,
      llm.Message{ Role: "user", Content: strings.Repeat("q", 400) },
      llm.Message{ Role: "assistant", Content: strings.Repeat("a", 400) },
    )
  }

  history = append(history, llm.Message{ Role: "user", Content: "now" })

  compacted := false
  hooks := Hooks{ Compacted: func(int, int) { compacted = true } }
  if _, err := loop.Turn(context.Background(), &history, hooks); err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if !summarized || !compacted {
    t.Fatalf("summarized=%v compacted=%v, want both", summarized, compacted)
  }

  if !strings.HasPrefix(history[1].Content, "[Summary") {
    t.Fatalf("history[1] = %q, want the summary", history[1].Content)
  }
}
