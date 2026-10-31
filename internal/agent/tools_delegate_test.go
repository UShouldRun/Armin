package agent

import (
  "context"
  "encoding/json"
  "io"
  "net/http"
  "net/http/httptest"
  "strings"
  "testing"

  "armin/internal/know"
  "armin/internal/llm"
)

// minionServer is a fake minion: it adds one fact to the tree, then
// answers. It records the tool names each request offered.
func minionServer(t *testing.T, offered *[]string) *llm.Client {
  t.Helper()
  round := 0
  mux := http.NewServeMux()
  mux.HandleFunc("/v1/chat/completions", func(
    w http.ResponseWriter, r *http.Request,
  ) {
    body, _ := io.ReadAll(r.Body)
    var req struct {
      Tools []llm.ToolDefinition `json:"tools"`
    }

    json.Unmarshal(body, &req)
    *offered = (*offered)[:0]
    for _, d := range req.Tools {
      *offered = append(*offered, d.Function.Name)
    }

    round++
    reply := llm.Message{
      Role: "assistant", Content: "the retry count is 2, in f.txt",
    }

    if round == 1 {
      reply = llm.Message{
        Role: "assistant",
        ToolCalls: []llm.ToolCall{ { Function: llm.ToolCallFunction{
          Name: "know_write",
          Arguments: map[string]any{
            "path": "loop/retries", "kind": "fact",
            "claim": "the loop retries twice",
            "confidence": "tested",
          },
        } } },
      }
    }

    writeChatResponse(t, w, reply)
  })

  server := httptest.NewServer(mux)
  t.Cleanup(server.Close)

  return llm.New(server.URL, "minion-model")
}

func delegateLoop(t *testing.T, offered *[]string) *Loop {
  t.Helper()
  l, _ := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {})
  l.Know = know.New()
  l.Session = "s1"
  minion := minionServer(t, offered)
  l.Minion = func(context.Context) (*llm.Client, error) { return minion, nil }
  l.SetRole(RoleCopilot)

  return l
}

func delegate(t *testing.T, l *Loop, args map[string]any) map[string]string {
  t.Helper()
  out := l.execute(context.Background(), llm.ToolCall{
    Function: llm.ToolCallFunction{ Name: "delegate", Arguments: args },
  }, Hooks{})
  m, ok := out.(map[string]string)
  if !ok {
    t.Fatalf("delegate returned %T: %v", out, out)
  }

  return m
}

func TestDelegatePassingCheck(t *testing.T) {
  var offered []string
  l := delegateLoop(t, &offered)
  out := delegate(t, l, map[string]any{
    "task": "find the retry count", "check": "command", "check_args": []any{
      "ls", "f.txt",
    },
  })

  if out["delegation"] != "1" || out["confidence"] != "checked" ||
    !strings.HasPrefix(out["check"], "passed: ls f.txt") ||
    !strings.Contains(out["answer"], "retry count is 2") {
    t.Fatalf("result: %v", out)
  }

  id, ok := l.Know.Lookup("loop/retries")
  if !ok {
    t.Fatal("the minion's node is missing")
  }

  n := l.Know.Node(id)
  a := l.Know.Author(n.Author)
  if n.Conf() != know.ConfChecked || a.Role != know.RoleMinion ||
      a.Delegation != 1 ||
    n.Flags & know.FlagVerified != 0 {
    t.Fatalf("node: conf %v, author %+v, flags %b", n.Conf(), a, n.Flags)
  }

  evs := l.Know.EvidenceOf(id, nil)
  if len(evs) != 1 || l.Know.Ev(evs[0]).Kind != know.RefCheck {
    t.Fatal("the passing check should be the node's evidence")
  }

  if !strings.Contains(out["nodes"], "minion-unverified") ||
      !strings.Contains(out["next"], "verified") {
    t.Fatalf("the delegator should be told to verify: %v", out)
  }

  if !contains(offered, "know_write") || contains(offered, "edit_file") ||
      contains(offered, "delegate") {
    t.Fatalf("minion offered %v", offered)
  }
}

func TestDelegateFailingCheck(t *testing.T) {
  var offered []string
  l := delegateLoop(t, &offered)
  out := delegate(t, l, map[string]any{
    "task": "find the retry count", "check": "command",
    "check_args": []any{ "ls", "no-such-file" },
  })

  if out["confidence"] != "inferred" ||
      !strings.HasPrefix(out["check"], "failed") {
    t.Fatalf("result: %v", out)
  }

  id, _ := l.Know.Lookup("loop/retries")
  if l.Know.Node(id).Conf() != know.ConfInferred {
    t.Fatal("a failed check must leave the minion's claim at inferred")
  }
}

func TestDelegateEditsAndRoles(t *testing.T) {
  var offered []string
  l := delegateLoop(t, &offered)
  delegate(
    t, l, map[string]any{ "task": "fix it", "check": "none", "edits": true },
  )
  if !contains(offered, "edit_file") {
    t.Fatalf("a copilot's minion with edits: %v", offered)
  }

  l.SetRole(RoleAdvisor)
  delegate(
    t, l, map[string]any{ "task": "fix it", "check": "none", "edits": true },
  )
  if contains(offered, "edit_file") {
    t.Fatal("an advisor's minion must not edit")
  }

  l.SetRole(RoleMinion)
  out := delegate(t, l, map[string]any{ "task": "x", "check": "none" })
  if !strings.Contains(out["error"], "minion role can't use delegate") {
    t.Fatalf("a minion delegating: %v", out)
  }
}

func TestDelegateOfferedOnlyWithAMinion(t *testing.T) {
  l := &Loop{}
  if l.offered(toolDelegate) {
    t.Fatal("delegate offered without a minion")
  }

  l.Minion = func(context.Context) (*llm.Client, error) { return nil, nil }
  if !l.offered(toolDelegate) {
    t.Fatal("delegate missing with a minion")
  }
}

func TestChoreClient(t *testing.T) {
  local := llm.New("http://local", "chat")
  minion := llm.New("http://minion", "small")
  l := &Loop{
    LLM: local,
    Minion: func(context.Context) (*llm.Client, error) { return minion, nil },
  }

  if l.ChoreClient(context.Background()).Model != "chat" {
    t.Fatal("a local chat model keeps its own chores")
  }

  hosted := llm.New("https://openrouter.ai/api/v1", "qwen")
  hosted.APIKey = "k"
  l.LLM = hosted
  if l.ChoreClient(context.Background()).Model != "small" {
    t.Fatal("a hosted chat model should hand chores to the minion")
  }
}

func contains(list []string, s string) bool {
  for _, x := range list {
    if x == s {
      return true
    }
  }

  return false
}
