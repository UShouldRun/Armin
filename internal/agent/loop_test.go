package agent

import (
  "context"
  "encoding/json"
  "fmt"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/tools"
)

func TestToolDefinitionsShape(t *testing.T) {
  defs := ToolDefinitions()
  if len(defs) != 38 {
    t.Fatalf("ToolDefinitions() has %d entries, want 38", len(defs))
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

  for _, want := range []string{
    "search_code", "read_file", "list_directory", "run_command", "web_search",
    "write_file", "edit_file", "read_pdf",
    "count_lines", "list_languages", "find_files", "file_stat", "find_todo_fixme",
    "env_info", "project_manifest",
    "find_definition", "list_symbols", "outline_file", "find_references",
    "git_log", "git_blame", "git_diff", "git_show", "working_tree_diff",
    "grep_history", "code_owners", "commit_stats",
    "run_tests", "build_check", "lint_check",
    "list_dependencies", "module_graph",
    "read_doc_comment",
    "find_callers", "find_dead_code", "impact_analysis",
    "semantic_search",
  } {
    if !names[want] {
      t.Errorf("ToolDefinitions() missing %q", want)
    }
  }
}

// writeChatResponse encodes msg as the OpenAI-shaped JSON
// llama-server's /v1/chat/completions returns — armin/internal/llm's
// own wire types are unexported, so tests outside that package
// build the shape by hand rather than reusing it. In particular a
// ToolCall's map[string]any Arguments becomes a JSON-encoded
// *string* here, the same translation llm.Client.Chat does when
// parsing a real response.
func writeChatResponse(t *testing.T, w http.ResponseWriter, msg llm.Message) {
  t.Helper()

  type wireFunc struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"`
  }

  type wireToolCall struct {
    ID       string   `json:"id"`
    Type     string   `json:"type"`
    Function wireFunc `json:"function"`
  }

  type wireMessage struct {
    Role      string         `json:"role"`
    Content   string         `json:"content"`
    ToolCalls []wireToolCall `json:"tool_calls,omitempty"`
  }

  wm := wireMessage{ Role: msg.Role, Content: msg.Content }
  for i, tc := range msg.ToolCalls {
    args, err := json.Marshal(tc.Function.Arguments)
    if err != nil {
      t.Fatal(err)
    }

    id := tc.ID
    if id == "" {
      id = fmt.Sprintf("call_%d", i)
    }

    wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
      ID: id, Type: "function",
      Function: wireFunc{ Name: tc.Function.Name, Arguments: string(args) },
    })
  }

  json.NewEncoder(w).Encode(map[string]any{
    "choices": []map[string]any{ { "message": wm } },
  })
}

func newTestLoop(t *testing.T, chatHandler http.HandlerFunc) (*Loop, string) {
  t.Helper()

  mux := http.NewServeMux()
  mux.HandleFunc("/v1/chat/completions", chatHandler)
  server := httptest.NewServer(mux)
  t.Cleanup(server.Close)

  root := t.TempDir()
  err := os.WriteFile(filepath.Join(root, "f.txt"), []byte("hello world"), 0o644)
  if err != nil {
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

    writeChatResponse(t, w, reply)
  })

  var toolCallSeen string
  onToolCall := func(name string, _ map[string]any) { toolCallSeen = name }

  history := []llm.Message{{Role: "system", Content: "sys"}}
  answer, err := loop.Turn(context.Background(), &history, Hooks{ ToolCall: onToolCall })
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

    writeChatResponse(t, w, reply)
  })

  history := []llm.Message{{Role: "system", Content: "sys"}}
  _, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if history[2].Role != "tool" {
    t.Fatalf("history[2] = %+v, want a tool-role error result", history[2])
  }

  var result map[string]string
  err = json.Unmarshal([]byte(history[2].Content), &result)
  if err != nil {
    t.Fatalf("tool error result isn't valid JSON: %v", err)
  }

  if result["error"] == "" {
    t.Fatal("expected the tool error result to carry a non-empty \"error\" field")
  }
}

func TestTurnHitsMaxRoundsThenSummarizes(t *testing.T) {
  calls := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    calls++
    var req struct {
      Tools []any `json:"tools"`
    }

    json.NewDecoder(r.Body).Decode(&req)
    if len(req.Tools) == 0 {
      // The closing call: no tools offered, so it must answer.
      writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: "partial report" })

      return
    }

    // Always asks for another tool call — a model that never stops.
    writeChatResponse(t, w, llm.Message{
      Role: "assistant",
      ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
        Name:      "list_directory",
        Arguments: map[string]any{},
      } } },
    })
  })
  loop.MaxRounds = 3

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  answer, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if answer != "partial report" || calls != 4 {
    t.Fatalf("answer = %q after %d calls, want the summary after 3+1", answer, calls)
  }
}

// TestTurnDispatchesWebSearchWithNilManager checks the wiring, not
// searxng itself: with Loop.WebSearch left nil (the default —
// cmd/armin/main.go only sets it when docker is found), a web_search
// call must come back as a structured tool-role error, same as any
// other tool failure, not a panic.
func TestTurnDispatchesWebSearchWithNilManager(t *testing.T) {
  round := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    var reply llm.Message
    if round == 1 {
      reply = llm.Message{
        Role: "assistant",
        ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{
          Name:      "web_search",
          Arguments: map[string]any{"query": "armin coding agent"},
        }}},
      }
    } else {
      reply = llm.Message{Role: "assistant", Content: "search is unavailable"}
    }

    writeChatResponse(t, w, reply)
  })

  history := []llm.Message{{Role: "system", Content: "sys"}}
  _, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if history[2].Role != "tool" {
    t.Fatalf("history[2] = %+v, want a tool-role error result", history[2])
  }

  var result map[string]string
  err = json.Unmarshal([]byte(history[2].Content), &result)
  if err != nil {
    t.Fatalf("tool error result isn't valid JSON: %v", err)
  }

  if !strings.Contains(result["error"], "docker") {
    t.Fatalf(`result["error"] = %q, want it to mention docker`, result["error"])
  }
}

func TestTurnRejectsUnknownTool(t *testing.T) {
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    reply := llm.Message{
      Role: "assistant",
      ToolCalls: []llm.ToolCall{{Function: llm.ToolCallFunction{Name: "delete_everything"}}},
    }

    writeChatResponse(t, w, reply)
  })

  history := []llm.Message{{Role: "system", Content: "sys"}}
  // The fake server always offers the same bogus tool call, so this
  // runs out the round limit — what matters is that execute()
  // doesn't panic on an unknown name but returns a structured error.
  loop.MaxRounds = 2
  _, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if !strings.Contains(history[2].Content, "unknown tool") {
    t.Fatalf("history[2] = %+v, want an unknown-tool error", history[2])
  }
}

// TestTurnBlocksARepeatedFailedCall: the same failing call twice in
// a turn is answered by the guard, not run again.
func TestTurnBlocksARepeatedFailedCall(t *testing.T) {
  round := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    round++
    reply := llm.Message{ Role: "assistant", Content: "giving up" }
    if round <= 2 {
      reply = llm.Message{
        Role: "assistant",
        ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
          Name:      "read_file",
          Arguments: map[string]any{ "path": "missing.txt" },
        } } },
      }
    }

    writeChatResponse(t, w, reply)
  })

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  _, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }

  second := history[4]
  if second.Role != "tool" || !strings.Contains(second.Content, "already made this exact call") {
    t.Fatalf("second result = %+v", second)
  }
}

func TestRequestAccessAsksThenGrants(t *testing.T) {
  loop, root := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {})
  outside := t.TempDir()
  os.WriteFile(filepath.Join(outside, "slides.pdf"), []byte("x"), 0o644)
  raw, _ := json.Marshal(map[string]string{ "dir": outside, "reason": "read slides" })

  out := loop.requestAccess(raw, Hooks{})
  if !strings.Contains(fmt.Sprint(out), "-add-dir") {
    t.Fatalf("with no Approve hook = %v", out)
  }

  asked := ""
  deny := Hooks{ Approve: func(tool, preview string) (bool, string) {
    asked = preview
    return false, ""
  } }
  out = loop.requestAccess(raw, deny)
  if !strings.Contains(fmt.Sprint(out), "declined") {
    t.Fatalf("declined = %v", out)
  }

  if !strings.Contains(asked, outside) || !strings.Contains(asked, "read slides") {
    t.Fatalf("question = %q", asked)
  }

  _, err := loop.Sandbox.ResolvePath(filepath.Join(outside, "slides.pdf"))
  if err == nil {
    t.Fatal("readable after a decline")
  }

  allow := Hooks{ Approve: func(string, string) (bool, string) { return true, "" } }
  granted, _ := loop.requestAccess(raw, allow).(map[string]any)
  if granted["granted"] == nil {
    t.Fatalf("granted = %v", granted)
  }

  _, err = loop.Sandbox.ResolvePath(filepath.Join(outside, "slides.pdf"))
  if err != nil {
    t.Fatalf("not readable after a grant: %v", err)
  }

  _ = root
}

// TestTurnAsksBeforeReadingOutside: a read outside the project makes
// armin ask the user before the call runs; a yes grants the folder
// and the read goes through, a no is remembered for the turn.
func TestTurnAsksBeforeReadingOutside(t *testing.T) {
  outside := t.TempDir()
  os.WriteFile(filepath.Join(outside, "notes.txt"), []byte("fox algorithm"), 0o644)
  target := filepath.Join(outside, "notes.txt")

  run := func(answer bool) ([]llm.Message, []string) {
    round := 0
    loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
      round++
      reply := llm.Message{ Role: "assistant", Content: "done" }
      if round <= 2 {
        reply = llm.Message{
          Role: "assistant",
          ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
            Name:      "read_file",
            Arguments: map[string]any{ "path": target, "start_line": float64(round) },
          } } },
        }
      }

      writeChatResponse(t, w, reply)
    })

    var asked []string
    hooks := Hooks{ Approve: func(tool, preview string) (bool, string) {
      asked = append(asked, preview)
      return answer, ""
    } }

    history := []llm.Message{ { Role: "system", Content: "sys" } }
    _, err := loop.Turn(context.Background(), &history, hooks)
    if err != nil {
      t.Fatalf("Turn: %v", err)
    }

    return history, asked
  }

  history, asked := run(true)
  if len(asked) != 1 || !strings.Contains(asked[0], outside) {
    t.Fatalf("asked = %q, want one question naming %s", asked, outside)
  }

  if !strings.Contains(history[2].Content, "fox algorithm") {
    t.Fatalf("read after the grant = %s", history[2].Content)
  }

  history, asked = run(false)
  if len(asked) != 1 {
    t.Fatalf("asked %d times after a decline, want 1", len(asked))
  }

  if !strings.Contains(history[2].Content, "declined") || !strings.Contains(history[4].Content, "declined") {
    t.Fatalf("results = %s / %s", history[2].Content, history[4].Content)
  }
}

func TestRunProgramNeedsApproval(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  in := tools.RunProgramInput{ Binary: "go", Args: []string{ "version" } }

  out := fmt.Sprint(loop.runProgram(context.Background(), in, Hooks{}))
  if !strings.Contains(out, "approval") {
    t.Fatalf("with nobody to ask = %s", out)
  }

  var asked string
  deny := Hooks{ Approve: func(tool, preview string) (bool, string) {
    asked = tool + ": " + preview
    return false, "not now"
  } }
  out = fmt.Sprint(loop.runProgram(context.Background(), in, deny))
  if !strings.Contains(out, "declined") || !strings.Contains(out, "not now") {
    t.Fatalf("declined = %s", out)
  }

  if asked != "run_program: $ go version" {
    t.Fatalf("question = %q", asked)
  }

  allow := Hooks{ Approve: func(string, string) (bool, string) { return true, "" } }
  res, ok := loop.runProgram(context.Background(), in, allow).(*tools.RunProgramOutput)
  if !ok || res.ExitCode != 0 || !strings.Contains(res.Output, "go version") {
    t.Fatalf("approved = %+v", res)
  }
}

// TestTurnStopsAModelThatKeepsRetrying replays a session: the user
// declines "make" with feedback, the model calls make again and
// again. The feedback must be in the error, and the turn must end in
// a tool-less answer after maxBlocked refusals, not run to the
// round limit.
func TestTurnStopsAModelThatKeepsRetrying(t *testing.T) {
  calls := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    calls++
    var req struct {
      Tools []any `json:"tools"`
    }

    json.NewDecoder(r.Body).Decode(&req)

    reply := llm.Message{ Role: "assistant", Content: "sorry, stopping" }
    if len(req.Tools) > 0 {
      reply = llm.Message{
        Role: "assistant",
        ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
          Name:      "run_program",
          Arguments: map[string]any{ "binary": "make" },
        } } },
      }
    }

    writeChatResponse(t, w, reply)
  })

  asked := 0
  hooks := Hooks{ Approve: func(string, string) (bool, string) {
    asked++
    return false, "this is C, run with gcc"
  } }

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  answer, err := loop.Turn(context.Background(), &history, hooks)
  if err != nil {
    t.Fatalf("Turn: %v", err)
  }

  if asked != 1 || answer != "sorry, stopping" {
    t.Fatalf("asked %d times, answer %q", asked, answer)
  }

  if calls != 1 + 1 + maxBlocked {
    t.Fatalf("model called %d times, want %d", calls, 2 + maxBlocked)
  }

  if !strings.Contains(history[2].Content, "run with gcc") {
    t.Fatalf("decline result = %s", history[2].Content)
  }
}

// TestPrivilegedProgramsAlwaysAsk: rm and friends reach the user as
// ApprovePrivileged, which no auto-approval may answer.
func TestPrivilegedProgramsAlwaysAsk(t *testing.T) {
  loop, root := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  var tool, preview string
  hooks := Hooks{ Approve: func(t, p string) (bool, string) {
    tool, preview = t, p
    return true, ""
  } }

  out := loop.runProgram(context.Background(), tools.RunProgramInput{ Binary: "rm", Args: []string{ "f.txt" } }, hooks)
  if tool != ApprovePrivileged || !AlwaysAsk(tool) || !strings.Contains(preview, "$ rm f.txt") {
    t.Fatalf("asked %q / %q", tool, preview)
  }

  res, ok := out.(*tools.RunProgramOutput)
  if !ok || res.ExitCode != 0 {
    t.Fatalf("rm = %+v", out)
  }

  _, err := os.Stat(filepath.Join(root, "f.txt"))
  if !os.IsNotExist(err) {
    t.Fatal("f.txt still there after an approved rm")
  }

  loop.runProgram(context.Background(), tools.RunProgramInput{ Binary: "go", Args: []string{ "version" } }, hooks)
  if AlwaysAsk(tool) {
    t.Fatalf("go version is asked as %q, want an ordinary run", tool)
  }
}

func TestNormalizeArgs(t *testing.T) {
  cases := []struct {
    in   map[string]any
    want string
  }{
    { map[string]any{ "binary": "find", "arguments": []any{ ".", "-name", "*.h" } }, `find [. -name *.h]` },
    { map[string]any{ "binary": "find", "args": `. -name "my file.h"` }, `find [. -name my file.h]` },
    { map[string]any{ "binary": "mpirun -np 4 ./fox" }, `mpirun [-np 4 ./fox]` },
    { map[string]any{ "binary": "make", "args": []any{ "run" } }, `make [run]` },
  }

  for _, c := range cases {
    call := llm.ToolCall{ Function: llm.ToolCallFunction{ Name: "run_program", Arguments: c.in } }
    normalizeArgs(&call)
    got := fmt.Sprint(call.Function.Arguments["binary"], " ", call.Function.Arguments["args"])
    if got != c.want {
      t.Errorf("normalizeArgs(%v) = %s, want %s", c.in, got, c.want)
    }
  }
}

func TestUnknownParamsAreAnError(t *testing.T) {
  loop, _ := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  out := fmt.Sprint(loop.execute(context.Background(), llm.ToolCall{ Function: llm.ToolCallFunction{
    Name: "read_file", Arguments: map[string]any{ "path": "f.txt", "lines": "1-5" },
  } }, Hooks{}))

  if !strings.Contains(out, "no parameter lines") || !strings.Contains(out, "start_line") {
    t.Fatalf("result = %s", out)
  }
}

// TestTurnStopsIdenticalReruns: make, make, make with the same
// "nothing to be done" each time is a loop.
func TestTurnStopsIdenticalReruns(t *testing.T) {
  calls := 0
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    calls++
    var req struct {
      Tools []any `json:"tools"`
    }

    json.NewDecoder(r.Body).Decode(&req)

    reply := llm.Message{ Role: "assistant", Content: "it's built" }
    if len(req.Tools) > 0 {
      reply = llm.Message{ Role: "assistant", ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
        Name: "run_command", Arguments: map[string]any{ "binary": "ls" },
      } } } }
    }

    writeChatResponse(t, w, reply)
  })

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  answer, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil || answer != "it's built" {
    t.Fatalf("Turn = %q, %v", answer, err)
  }

  // ls, ls (same: blocked 1), ls (same: blocked 2, stop), summary.
  if calls != 4 {
    t.Fatalf("model called %d times, want 4", calls)
  }
}

// TestRerunAfterOtherCallsIsNoted: the same run with the same output,
// with another call in between (a rebuild, a read), runs and gets a
// note; the turn goes on. Only back to back is it a loop.
func TestRerunAfterOtherCallsIsNoted(t *testing.T) {
  script := []string{ "ls", "date", "ls", "date", "ls" }
  calls := 0
  var results []string
  loop, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    var req struct {
      Messages []llm.Message `json:"messages"`
      Tools    []any         `json:"tools"`
    }

    json.NewDecoder(r.Body).Decode(&req)
    last := req.Messages[len(req.Messages) - 1]
    if last.Role == "tool" {
      results = append(results, last.Content)
    }

    reply := llm.Message{ Role: "assistant", Content: "done" }
    if calls < len(script) {
      reply = llm.Message{ Role: "assistant", ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
        Name: "run_command", Arguments: map[string]any{ "binary": script[calls] },
      } } } }
    }

    calls++
    writeChatResponse(t, w, reply)
  })

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  answer, err := loop.Turn(context.Background(), &history, Hooks{})
  if err != nil || answer != "done" {
    t.Fatalf("Turn = %q, %v", answer, err)
  }

  if calls != len(script) + 1 {
    t.Fatalf("model called %d times, want %d: the turn was stopped", calls, len(script) + 1)
  }

  third := results[2]
  if !strings.Contains(third, "same output") || strings.Contains(third, "\"error\"") {
    t.Fatalf("third ls result = %s, want a note, not an error", third)
  }
}

// TestScriptOf: only an interpreter's text script is shown, never a
// data file (rm a.c) or a binary (mpirun ./fox, python3 blob).
func TestScriptOf(t *testing.T) {
  loop, root := newTestLoop(t, func(http.ResponseWriter, *http.Request) {})
  os.WriteFile(filepath.Join(root, "gen.py"), []byte("print(1)\n"), 0o644)
  os.WriteFile(filepath.Join(root, "a.c"), []byte("int x;\n"), 0o644)
  os.WriteFile(filepath.Join(root, "fox"), []byte("\x7fELF\x00\x00"), 0o755)

  cases := []struct {
    in   tools.RunProgramInput
    want string
  }{
    { tools.RunProgramInput{ Binary: "python3", Args: []string{ "gen.py" } }, "gen.py" },
    { tools.RunProgramInput{ Binary: "rm", Args: []string{ "a.c" } }, "" },
    { tools.RunProgramInput{ Binary: "mpirun", Args: []string{ "-np", "1", "./fox" } }, "" },
    { tools.RunProgramInput{ Binary: "python3", Args: []string{ "fox" } }, "" },
    { tools.RunProgramInput{ Binary: "bash", Args: []string{ "-c", "make && ./fox" } }, "" },
  }

  for _, c := range cases {
    got := loop.scriptOf(c.in, root)
    if filepath.Base(got) != c.want && !(got == "" && c.want == "") {
      t.Errorf("%s %v → %q, want %q", c.in.Binary, c.in.Args, got, c.want)
    }
  }
}
