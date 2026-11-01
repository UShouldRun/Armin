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
  g.AddNode(
    tools, "golden",
    "each role's tool list is pinned in " +
      "testdata/definitions.<role>.json, regenerated with -update",
    know.KindFact, know.ConfChecked, 0,
  )

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

  history = append(
    history,
    llm.Message{
      Role: "assistant", Content: "testdata/definitions.advisor.json [n2]",
    },
  )
  l.recallSettle(history, 2)
  snap := metrics.Snapshot()
  if snap["recall.attached"].Count != 1 || snap["recall.cited"].Count != 1 {
    t.Fatalf("metrics: %+v", snap)
  }

  // a read of a recalled node's file, no citation: read, not cited
  l.recalledFiles = append(l.recalledFiles, "testdata/x.json")
  history[2] = llm.Message{
    Role: "assistant",
    ToolCalls: []llm.ToolCall{
      {
        Function: llm.ToolCallFunction{
          Name: "read_file", Arguments: map[string]any{ "path": "testdata/x.json" },
        },
      },
    },
  }
  l.recallSettle(history, 2)
  snap = metrics.Snapshot()
  if snap["recall.cited"].Count != 1 || snap["recall.read"].Count != 1 {
    t.Fatalf("a read without a citation: %+v", snap)
  }

  // a know_write deriving from the recalled node by path: cited
  history[2] = llm.Message{
    Role: "assistant",
    ToolCalls: []llm.ToolCall{
      {
        Function: llm.ToolCallFunction{
          Name: "know_write", Arguments: map[string]any{
            "path": "tools/more", "derived_from": []any{ "tools/golden" },
          },
        },
      },
    },
  }
  l.recallSettle(history, 2)
  if metrics.Snapshot()["recall.cited"].Count != 2 {
    t.Fatal("a know_write building on a recalled node wasn't a citation")
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
  if metrics.Snapshot()["recall.cited"].Count != 0 {
    t.Fatal("cited counted with nothing attached")
  }
}

func TestRecallHidesReadTools(t *testing.T) {
  l := &Loop{ Know: know.New(), Recall: true }
  names := toolNames(l)
  if names["know_find"] || names["know_trace"] || !names["know_write"] {
    t.Fatalf("with recall: %v", names)
  }
}

// TestRecallIgnoresTheRoleReminder: after a role switch the reminder
// goes in front of the message, but recall searches the message alone
// and the reminder ends up first.
func TestRecallIgnoresTheRoleReminder(t *testing.T) {
  l := recallLoop(t)
  l.SetRole(RoleAdvisor) // its reminder talks about decisions and files
  history := []llm.Message{ { Role: "user", Content: "zzz qqq" } }
  l.recall(history)
  l.takeReminder(history)
  got := history[0].Content
  if strings.Contains(got, recallHead) {
    t.Fatalf("the reminder's words were searched:\n%s", got)
  }

  if !strings.HasPrefix(got, "[armin] Your role is now advisor") {
    t.Fatalf("the reminder should come first:\n%s", got)
  }
}
