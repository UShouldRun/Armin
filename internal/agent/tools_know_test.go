package agent

import (
  "context"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/notes"
  "armin/internal/sandbox"
)

// knowLoop is a Loop on a temp project with an in-memory tree and a
// call helper that returns the result's fields.
func knowLoop(t *testing.T) (
  *Loop, func(string, map[string]any) map[string]string,
) {
  t.Helper()
  root := t.TempDir()
  os.WriteFile(filepath.Join(root, "conn.go"),
    []byte("package tcp\n\nfunc handshake() {\n  syn()\n  ack()\n}\n"), 0o644)
  sb, err := sandbox.New(root, sandbox.Options{})
  if err != nil {
    t.Fatal(err)
  }

  l := &Loop{
    Sandbox: sb,
    Know: know.New(),
    Session: "s1",
    LLM: &llm.Client{ Model: "m" },
  }
  call := func(name string, args map[string]any) map[string]string {
    t.Helper()
    out := l.execute(context.Background(), llm.ToolCall{
      Function: llm.ToolCallFunction{ Name: name, Arguments: args },
    }, Hooks{})
    m, ok := out.(map[string]string)
    if !ok {
      t.Fatalf("%s returned %T: %v", name, out, out)
    }

    return m
  }

  return l, call
}

func TestKnowToolsOfferedOnlyWithATree(t *testing.T) {
  l := &Loop{}
  if l.offered(toolKnowFind) {
    t.Fatal("know tools without a tree")
  }

  l.Know = know.New()
  if !l.offered(toolKnowFind) || !l.offered(toolKnowAdd) {
    t.Fatal("know tools missing with a tree")
  }
}

func TestKnowAddFindGet(t *testing.T) {
  l, call := knowLoop(t)

  out := call("know_add", map[string]any{
    "path": "tcp/handshake", "kind": "fact",
    "claim": "the client sends SYN, then ACK", "confidence": "checked",
    "evidence": []any{
      "conn.go:3-6", "https://www.rfc-editor.org/rfc/rfc9293",
    },
  })
  if !strings.HasPrefix(out["added"], "n2 tcp/handshake fact checked") {
    t.Fatalf("add: %v", out)
  }

  tcp, _ := l.Know.Lookup("tcp")
  if l.Know.Node(tcp).Kind() != know.KindTopic {
    t.Fatal("the missing parent should be added as a topic")
  }

  hs, _ := l.Know.Lookup("tcp/handshake")
  evs := l.Know.EvidenceOf(hs, nil)
  if len(evs) != 2 || l.Know.Ev(evs[0]).Kind != know.RefFile ||
    l.Know.Ev(evs[0]).Hash == 0 || l.Know.Ev(evs[1]).Kind != know.RefURL {
    t.Fatalf("evidence: %+v", evs)
  }

  out = call("know_find", map[string]any{ "query": "handshake syn" })
  if !strings.HasPrefix(out["found"], "n2 tcp/handshake") {
    t.Fatalf("find: %v", out)
  }

  out = call("know_get", map[string]any{ "ids": []any{ "n2" } })
  if !strings.Contains(out["nodes"], "by copilot m") ||
    !strings.Contains(out["nodes"], "evidence file conn.go:3-6") {
    t.Fatalf("get: %v", out)
  }
}

func TestKnowAddHoldsBackSimilar(t *testing.T) {
  _, call := knowLoop(t)
  call("know_add", map[string]any{
    "path": "tcp/handshake", "kind": "fact",
    "claim": "the client sends SYN then ACK",
  })

  out := call("know_add", map[string]any{
    "path": "tcp/opening", "kind": "fact",
    "claim": "the client sends SYN then ACK first",
  })
  if out["similar"] == "" || !strings.Contains(out["next"], "force") {
    t.Fatalf("similar not held back: %v", out)
  }

  out = call("know_add", map[string]any{
    "path": "tcp/opening", "kind": "fact",
    "claim": "the client sends SYN then ACK first",
    "force": true,
  })
  if out["added"] == "" {
    t.Fatalf("force: %v", out)
  }
}

func TestKnowWritingRules(t *testing.T) {
  _, call := knowLoop(t)
  out := call(
    "know_add",
    map[string]any{ "path": "tcp/rtt", "kind": "concept", "claim": "" },
  )
  if !strings.Contains(out["error"], "definition") {
    t.Fatalf("a concept without a definition: %v", out)
  }

  out = call("know_add", map[string]any{
    "path": "x", "kind": "fact", "claim": "y", "needs": []any{ "n99" },
  })
  if !strings.Contains(out["error"], "no node n99") {
    t.Fatalf("a dangling reference: %v", out)
  }
}

func TestKnowMinionLimits(t *testing.T) {
  l, call := knowLoop(t)
  l.SetRole(RoleMinion)

  out := call("know_add", map[string]any{
    "path": "tcp/handshake", "kind": "fact", "claim": "SYN then ACK",
    "confidence": "tested",
  })
  if !strings.Contains(out["added"], "fact inferred minion-unverified") ||
    !strings.Contains(out["note"], "capped") {
    t.Fatalf("minion add: %v", out)
  }

  out = call(
    "know_add",
    map[string]any{ "path": "d", "kind": "decision", "claim": "use x" },
  )
  if !strings.Contains(out["error"], "decisions belong") {
    t.Fatalf("minion decision: %v", out)
  }

  out = call("know_update", map[string]any{ "id": "n2", "verified": true })
  if !strings.Contains(out["error"], "only the copilot or the advisor") {
    t.Fatalf("minion verify: %v", out)
  }

  l.SetRole(RoleCopilot)
  out = call(
    "know_update", map[string]any{ "id": "tcp/handshake", "verified": true },
  )
  if strings.Contains(out["now"], "minion-unverified") || out["now"] == "" {
    t.Fatalf("copilot verify: %v", out)
  }

  hs, _ := l.Know.Lookup("tcp/handshake")
  a := l.Know.Author(l.Know.Node(hs).Author)
  if a.Role != know.RoleMinion {
    t.Fatal("verifying must keep the minion's authorship")
  }
}

func TestKnowLinkUpdateTrace(t *testing.T) {
  l, call := knowLoop(t)
  call(
    "know_add",
    map[string]any{ "path": "a", "kind": "fact", "claim": "alpha base fact" },
  )
  call("know_add", map[string]any{
    "path": "b", "kind": "fact", "claim": "beta conclusion",
    "derived_from": []any{ "a" },
  })

  out := call(
    "know_link", map[string]any{ "from": "b", "to": "a", "kind": "explains" },
  )
  if !strings.Contains(out["now"], "needs n1") ||
      !strings.Contains(out["now"], "explains n1") {
    t.Fatalf("link: %v", out)
  }

  out = call("know_trace", map[string]any{ "id": "b" })
  if !strings.Contains(out["trace"], "depth 1: n1 a fact") {
    t.Fatalf("trace: %v", out)
  }

  a, _ := l.Know.Lookup("a")
  l.Know.SetFlags(a, know.FlagStale, 0)
  out = call("know_update", map[string]any{
    "id": "a", "claim": "alpha, re-read", "current": true, "move_to": "base/a",
  })
  if strings.Contains(out["now"], "STALE") ||
      !strings.Contains(out["now"], "base/a") {
    t.Fatalf("update: %v", out)
  }

  out = call("know_update", map[string]any{ "id": "b", "delete": true })
  if out["deleted"] != "b" || l.Know.Live(2) {
    t.Fatalf("delete: %v", out)
  }
}

func TestParseRange(t *testing.T) {
  for ref, want := range map[string]bool{
    "a/b.go:12-40": true, "a/b.go:7": true, "a.go:0-3": false,
    "a.go:9-3": false, "https://x.org/a:b": false, "a.go": false,
  } {
    _, _, _, ok := know.ParseRange(ref)
    if ok != want {
      t.Errorf("ParseRange(%q) = %v", ref, ok)
    }
  }
}

func TestCitesCode(t *testing.T) {
  l := &Loop{} // no symbol index: only file:line counts
  for text, want := range map[string]bool{
    "the loop retries twice (internal/agent/turn.go:120)": true,
    "see turn.go:88 for the guard":                        true,
    "the user wants 2-space indentation":                  false,
    "keep tests in test/ and use `go test`":               false,
    "meet at 10:30 on Fridays":                            false,
    "prefer `strings.Builder` over += in loops":           false,
  } {
    if l.citesCode(context.Background(), text) != want {
      t.Errorf("citesCode(%q) = %v", text, !want)
    }
  }
}

func TestMemorySaveRefusesCodeFacts(t *testing.T) {
  l, call := knowLoop(t)
  l.Memories = &notes.Memories{ ProjectDir: t.TempDir(), UserDir: t.TempDir() }

  out := call("memory_save", map[string]any{
    "name": "retries", "description": "retry count",
    "content": "the loop retries twice, see turn.go:120",
  })
  if !strings.Contains(out["error"], "know_add") {
    t.Fatalf("a code fact was saved as a memory: %v", out)
  }

  out = call("memory_save", map[string]any{
    "name": "indent", "description": "style",
    "content": "use 2-space indentation",
  })
  if out["saved"] != "indent" {
    t.Fatalf("a preference was refused: %v", out)
  }
}

func TestKnowLoopReportedAndReminded(t *testing.T) {
  l, call := knowLoop(t)
  call(
    "know_add",
    map[string]any{ "path": "a", "kind": "concept", "claim": "alpha thing" },
  )
  call("know_add", map[string]any{
    "path": "b", "kind": "concept", "claim": "beta thing", "needs": []any{
      "a",
    },
  })

  out := call(
    "know_link", map[string]any{ "from": "a", "to": "b", "kind": "requires" },
  )
  if !strings.Contains(out["cycle"], "n1 → n2") ||
      !strings.Contains(out["cycle"], "merge") {
    t.Fatalf("loop not reported: %v", out)
  }

  history := []llm.Message{ { Role: "user", Content: "x" } }
  tn := &turn{ l: l, history: &history }
  if !tn.cycleCheck() ||
      !strings.Contains(history[1].Content, "cycles/loop-1-2") {
    t.Fatalf("no reminder: %v", history)
  }

  if tn.cycleCheck() {
    t.Fatal("the reminder comes once per turn")
  }

  out = call(
    "know_link",
    map[string]any{
      "from": "a", "to": "b", "kind": "requires", "remove": true,
    },
  )
  if out["cycle"] != "no requires loops are open now" {
    t.Fatalf("resolution not seen: %v", out)
  }

  tn = &turn{ l: l, history: &history }
  if tn.cycleCheck() {
    t.Fatal("reminded with nothing open")
  }
}

func TestMinionLeavesLoops(t *testing.T) {
  l, call := knowLoop(t)
  l.SetRole(RoleMinion)
  call(
    "know_add",
    map[string]any{ "path": "a", "kind": "concept", "claim": "alpha thing" },
  )
  call("know_add", map[string]any{
    "path": "b", "kind": "concept", "claim": "beta thing", "needs": []any{
      "a",
    },
  })

  out := call(
    "know_link", map[string]any{ "from": "a", "to": "b", "kind": "requires" },
  )
  if !strings.Contains(out["cycle"], "Leave it") {
    t.Fatalf("minion told to resolve: %v", out)
  }

  history := []llm.Message{ { Role: "user", Content: "x" } }
  if (&turn{ l: l, history: &history }).cycleCheck() {
    t.Fatal("the minion was asked to resolve a loop")
  }
}
