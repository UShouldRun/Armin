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

func TestWithModelLine(t *testing.T) {
  p := WithModelLine("base prompt", "coding", "Mellum2.1-12B")
  if p != "base prompt\n\nYou are running as model `coding` (Mellum2.1-12B)." {
    t.Fatalf("first = %q", p)
  }

  p = WithModelLine(p, "strong", "Qwen3.6-35B-A3B on EC2 instance i-1")
  if strings.Count(p, modelLinePrefix) != 1 || !strings.Contains(p, "`strong` (Qwen3.6-35B-A3B on EC2 instance i-1)") {
    t.Fatalf("replaced = %q", p)
  }

  got := WithModelLine("x", "chat", "chat")
  if !strings.HasSuffix(got, "model `chat`.") {
    t.Fatalf("label equal to name = %q", got)
  }
}

func TestDecide(t *testing.T) {
  cases := []struct {
    name string
    s    ContextState
    want Action
  }{
    { "no window", ContextState{ Used: 99999 }, 0 },
    { "plenty of room", ContextState{ Used: 40000, Window: 98304, Resident: 98304, Max: 196608 }, 0 },
    { "past half: trim only", ContextState{ Used: 60000, Window: 98304, Resident: 98304, Max: 196608 }, ActTrim },
    {
      "full, mostly old history: save and compact",
      ContextState{ Used: 80000, Turn: 10000, Window: 98304, Resident: 98304, Max: 196608 },
      ActTrim | ActSave | ActCompact,
    },
    {
      "full, mostly this turn, can grow: grow",
      ContextState{ Used: 80000, Turn: 60000, Window: 98304, Resident: 98304, Max: 196608 },
      ActTrim | ActGrow,
    },
    {
      "full, mostly this turn, no stream tier: compact",
      ContextState{ Used: 80000, Turn: 60000, Window: 98304, Resident: 98304, Max: 98304 },
      ActTrim | ActSave | ActCompact,
    },
    {
      "fixed overhead doesn't count against the working set",
      ContextState{ Used: 80000, Turn: 30000, Fixed: 25000, Window: 98304, Resident: 98304, Max: 196608 },
      ActTrim | ActGrow,
    },
    {
      "full while already streaming: compact",
      ContextState{ Used: 160000, Turn: 150000, Window: 196608, Resident: 98304, Max: 196608, Streaming: true },
      ActTrim | ActSave | ActCompact,
    },
  }

  for _, c := range cases {
    got := Decide(c.s)
    if got != c.want {
      t.Errorf("%s: Decide = %05b, want %05b", c.name, got, c.want)
    }
  }

  shrink := ContextState{ Used: 20000, Window: 196608, Resident: 98304, Max: 196608, Streaming: true }
  if DecideAfterCompact(shrink) != ActShrink {
    t.Error("expected a shrink once compaction left little")
  }

  shrink.Used = 70000
  if DecideAfterCompact(shrink) != 0 {
    t.Error("shrank although the context wouldn't fit the resident window comfortably")
  }
}

// fakeWindow records tier changes.
type fakeWindow struct {
  resident, max int
  streaming     bool
  calls         []string
}

func (w *fakeWindow) Size() int {
  if w.streaming {
    return w.max
  }

  return w.resident
}

func (w *fakeWindow) Resident() int    { return w.resident }
func (w *fakeWindow) Max() int         { return w.max }
func (w *fakeWindow) Streaming() bool  { return w.streaming }
func (w *fakeWindow) Grow(context.Context) (int, error) {
  w.calls = append(w.calls, "grow")
  w.streaming = true

  return w.max, nil
}

func (w *fakeWindow) Shrink(context.Context) (int, error) {
  w.calls = append(w.calls, "shrink")
  w.streaming = false

  return w.resident, nil
}

func TestGrowsWhenTheCurrentTurnIsTheBulk(t *testing.T) {
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "done" })
  })
  win := &fakeWindow{ resident: 2000, max: 8000 }
  loop.Window = win

  var notices []string
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "refactor the parser" },
    { Role: "assistant", ToolCalls: []llm.ToolCall{ call("a", "read_file", map[string]any{ "path": "p.go" }) } },
    { Role: "tool", ToolName: "read_file", ToolCallID: "a", Content: strings.Repeat("x", 7000) },
  }

  loop.manageContext(context.Background(), &history, llm.Usage{}, Hooks{
    Notice: func(s string) { notices = append(notices, s) },
  })

  if strings.Join(win.calls, ",") != "grow" {
    t.Fatalf("window calls = %v, want grow", win.calls)
  }

  if history[3].Content[0] != 'x' {
    t.Fatal("the working set was trimmed instead of kept")
  }

  if len(notices) == 0 || !strings.Contains(notices[0], "KV streaming") {
    t.Fatalf("notices = %v", notices)
  }
}

func TestCompactsOldHistoryAfterSavingAndShrinksBack(t *testing.T) {
  var sawSaveNote, summarized bool
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    var req struct {
      Tools    []struct{ Function struct{ Name string } } `json:"tools"`
      Messages []llm.Message                              `json:"messages"`
    }

    json.NewDecoder(r.Body).Decode(&req)
    lastMsg := req.Messages[len(req.Messages) - 1]
    switch {
    case lastMsg.Content == saveNote:
      sawSaveNote = true
      for _, d := range req.Tools {
        n := d.Function.Name
        if n != "scratchpad_write" && n != "scratchpad_read" && n != "memory_save" {
          t.Errorf("save turn offered %s", n)
        }
      }

      writeChatResponse(t, w, llm.Message{ Role: "assistant", ToolCalls: []llm.ToolCall{
        call("s1", "scratchpad_write", map[string]any{ "name": "plan", "content": "step 3 of 5" }),
      } })
    case lastMsg.Role == "tool" && lastMsg.ToolName == "scratchpad_write":
      writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "saved" })
    case len(req.Tools) == 0:
      summarized = true
      writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "summary" })
    }
  })
  loop.Scratch = &notes.Scratchpad{ Dir: t.TempDir() }
  win := &fakeWindow{ resident: 9000, max: 14000, streaming: true }
  loop.Window = win

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  for i := 0; i < 12; i++ {
    history = append(history,
      llm.Message{ Role: "user", Content: strings.Repeat("q", 2000) },
      llm.Message{ Role: "assistant", Content: strings.Repeat("a", 2000) },
    )
  }

  history = append(history, llm.Message{ Role: "user", Content: "next task" })

  loop.manageContext(context.Background(), &history, llm.Usage{}, Hooks{})

  if !sawSaveNote || !summarized {
    t.Fatalf("saveNote=%v summarized=%v, want both", sawSaveNote, summarized)
  }

  plan, _, err := loop.Scratch.Read("plan", 0, 0)
  if err != nil || !strings.Contains(plan, "step 3") {
    t.Fatalf("plan = %q, %v", plan, err)
  }

  for _, m := range history {
    if m.Content == saveNote {
      t.Fatal("the save exchange leaked into history")
    }
  }

  if strings.Join(win.calls, ",") != "shrink" {
    t.Fatalf("window calls = %v, want shrink after compaction", win.calls)
  }
}

func TestTrimArchivesToScratchpadAndKeepsSkills(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  loop.Scratch = &notes.Scratchpad{ Dir: t.TempDir() }
  loop.KeepTurns = 1

  big := strings.Repeat("r", 1000)
  skill := strings.Repeat("s", 1000)
  msgs := []llm.Message{
    { Role: "assistant", ToolCalls: []llm.ToolCall{
      call("a", "web_search", map[string]any{ "query": "go generics" }),
      call("b", "use_skill", map[string]any{ "name": "go-style" }),
    } },
    { Role: "tool", ToolName: "web_search", ToolCallID: "a", Content: big },
    { Role: "tool", ToolName: "use_skill", ToolCallID: "b", Content: skill },
  }

  loop.elide(msgs, true)

  var stub map[string]any
  err := json.Unmarshal([]byte(msgs[1].Content), &stub)
  if err != nil {
    t.Fatalf("not a stub: %q", msgs[1].Content)
  }

  ref, _ := stub["archived"].(string)
  if !strings.HasPrefix(ref, "scratchpad_read name=archive-1") {
    t.Fatalf("stub = %v, want a pointer into the archive", stub)
  }

  got, _, _ := loop.Scratch.Read("archive-1", 0, 0)
  if !strings.Contains(got, big) || !strings.Contains(got, "web_search") {
    t.Fatal("the archive doesn't hold the original result")
  }

  if msgs[2].Content != skill {
    t.Fatal("a pinned skill result was trimmed")
  }
}

func TestEndTurnArchivesThinking(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  dir := t.TempDir()
  loop.Scratch = &notes.Scratchpad{ Dir: dir }

  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "why is the build slow" },
    { Role: "assistant", Content: "looking", Reasoning: "check the cache flags first" },
  }

  loop.endTurn(&history, 2, Hooks{})

  if history[2].Reasoning != "" {
    t.Fatal("reasoning stayed in context")
  }

  raw, err := os.ReadFile(filepath.Join(dir, "thinking.md"))
  if err != nil || !strings.Contains(string(raw), "check the cache flags first") ||
    !strings.Contains(string(raw), "why is the build slow") {
    t.Fatalf("thinking.md = %q, %v", raw, err)
  }
}

func TestSuggestCompact(t *testing.T) {
  cases := []struct {
    name string
    s    ContextState
    want bool
  }{
    { "no window", ContextState{ Used: 99999 }, false },
    { "below the mark", ContextState{ Used: 55000, Window: 100000 }, false },
    { "between the mark and auto-compaction", ContextState{ Used: 65000, Window: 100000 }, true },
    { "auto-compaction's turn", ContextState{ Used: 80000, Window: 100000 }, false },
  }

  for _, c := range cases {
    got := SuggestCompact(c.s)
    if got != c.want {
      t.Errorf("%s: SuggestCompact = %v, want %v", c.name, got, c.want)
    }
  }
}

func TestCompactHintOncePerStretch(t *testing.T) {
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {})
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: strings.Repeat("q", 8000) },
    { Role: "assistant", Content: strings.Repeat("a", 8000) },
    { Role: "user", Content: "next" },
  }
  loop.ContextSize = loop.Estimate(history) * 100 / 67

  hints := func() int {
    n := 0
    for _, m := range history {
      if strings.HasPrefix(m.Content, compactHintPrefix) {
        n++
      }
    }

    return n
  }

  var notices []string
  hooks := Hooks{ Notice: func(s string) { notices = append(notices, s) } }
  loop.hintCompact(&history, hooks)
  if hints() != 1 || len(notices) != 1 {
    t.Fatalf("hints = %d, notices = %v; want one of each", hints(), notices)
  }

  if !strings.Contains(history[len(history) - 1].Content, "/compact") {
    t.Fatalf("hint = %q, want it to name /compact", history[len(history) - 1].Content)
  }

  history = append(history,
    llm.Message{ Role: "assistant", Content: "done" },
    llm.Message{ Role: "user", Content: "more" },
  )
  loop.hintCompact(&history, hooks)
  if hints() != 1 {
    t.Fatalf("hints = %d after a second turn, want it not repeated", hints())
  }

  // A compaction starts a new stretch: the hint may come back.
  history = append(history,
    llm.Message{ Role: "assistant", Content: "ok" },
    llm.Message{ Role: "user", Content: "[Summary of the earlier conversation, compacted to save context]\n- x" },
    llm.Message{ Role: "user", Content: "again" },
  )
  loop.hintCompact(&history, hooks)
  if hints() != 2 {
    t.Fatalf("hints = %d after a compaction, want a new one", hints())
  }
}
