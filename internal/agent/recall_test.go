package agent

import (
  "strings"
  "testing"

  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/metrics"
)

func recallLoop(t *testing.T) *Loop {
  t.Helper()
  g := know.New()
  tools, _ := g.AddNode(0, "tools", "", know.KindTopic, know.ConfChecked, 0)
  g.AddNode(tools, "golden", "each role's tool list is pinned in "+
    "testdata/definitions.<role>.json, regenerated with -update",
    know.KindFact, know.ConfChecked, 0)

  return &Loop{ Know: g, Recall: true }
}

func TestRecallAttachesAndSettles(t *testing.T) {
  metrics.Reset()
  l := recallLoop(t)
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "which file pins the role tool list golden?" },
  }

  l.recall(history)
  got := history[1].Content
  if !strings.HasPrefix(got, recallHead + "n2 tools/golden fact checked") ||
    !strings.HasSuffix(got, "\n\nwhich file pins the role tool list golden?") {
    t.Fatalf("attached:\n%s", got)
  }

  if history[0].Content != "sys" {
    t.Fatal("the system prompt must not change")
  }

  history = append(history, llm.Message{
    Role: "assistant", Content: "testdata/definitions.advisor.json [n2]",
  })
  l.recallSettle(history, 2)
  snap := metrics.Snapshot()
  if snap["recall.attached"].Count != 1 || snap["recall.used"].Count != 1 {
    t.Fatalf("metrics: %+v", snap)
  }
}

func TestRecallNothingMatches(t *testing.T) {
  metrics.Reset()
  l := recallLoop(t)
  history := []llm.Message{ { Role: "user", Content: "zzz qqq" } }
  l.recall(history)
  empty := metrics.Snapshot()["recall.empty"].Count
  if history[0].Content != "zzz qqq" || empty != 1 {
    t.Fatalf("nothing should be attached: %q", history[0].Content)
  }

  l.recallSettle(history, 0)
  if metrics.Snapshot()["recall.used"].Count != 0 {
    t.Fatal("used counted with nothing attached")
  }
}

func TestRecallHidesReadTools(t *testing.T) {
  l := &Loop{ Know: know.New(), Recall: true }
  names := toolNames(l)
  if names["know_find"] || names["know_trace"] || !names["know_write"] {
    t.Fatalf("with recall: %v", names)
  }
}
