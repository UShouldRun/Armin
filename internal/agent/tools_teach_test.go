package agent

import (
  "context"
  "strings"
  "testing"
  "time"

  "armin/internal/learner"
  "armin/internal/llm"
)

func teachLoop(t *testing.T) (*Loop, func(map[string]any) map[string]string) {
  t.Helper()
  l := &Loop{
    Learner: learner.New(), Session: "s1", LLM: &llm.Client{ Model: "m" },
  }
  l.SetRole(RoleAdvisor)
  call := func(args map[string]any) map[string]string {
    t.Helper()
    out := l.execute(
      context.Background(),
      llm.ToolCall{
        Function: llm.ToolCallFunction{
          Name: "learn_explained", Arguments: args,
        },
      },
      Hooks{},
    )
    m, ok := out.(map[string]string)
    if !ok {
      t.Fatalf("returned %T: %v", out, out)
    }

    return m
  }

  return l, call
}

func TestLearnExplained(t *testing.T) {
  l, call := teachLoop(t)
  out := call(
    map[string]any{
      "define": []any{
        "tcp/handshake: three segments open a connection",
        "tcp/seqnum: each byte gets a number",
        "tcp/window: how much the receiver can take",
      },
      "correct":        []any{ "tcp/handshake" },
      "misconceptions": []any{
        "tcp/seqnum: thinks segments are numbered, not bytes",
      },
      "gaps":           []any{ "tcp/window" },
    },
  )

  if out["recorded"] != "1 correct, 1 misconceptions, 1 gaps" ||
    !strings.Contains(
      out["misconceptions"], "tcp/seqnum/misconception-1 question",
    ) {
    t.Fatalf("result: %v", out)
  }

  tree := l.Learner.Tree
  today := learner.Day(timeNow())
  hs, _ := tree.Lookup("tcp/handshake")
  seq, _ := tree.Lookup("tcp/seqnum")
  win, _ := tree.Lookup("tcp/window")
  if l.Learner.State(hs, today) != learner.Known ||
      l.Learner.State(seq, today) != learner.Unknown ||
    l.Learner.State(win, today) != learner.NotSure {
    t.Fatalf(
      "states: %v %v %v",
      l.Learner.State(hs, today),
      l.Learner.State(
        seq, today,
      ),
      l.Learner.State(win, today),
    )
  }

  out = call(map[string]any{ "correct": []any{ "tcp/congestion" } })
  if !strings.Contains(out["error"], "add it with define") {
    t.Fatalf("an unknown concept: %v", out)
  }
}

func TestLearnExplainedIsTheAdvisors(t *testing.T) {
  l, call := teachLoop(t)
  l.SetRole(RoleCopilot)
  out := call(map[string]any{ "correct": []any{ "x" } })
  if !strings.Contains(out["error"], "copilot role can't use learn_explained") {
    t.Fatalf("copilot: %v", out)
  }
}

func timeNow() time.Time { return time.Now() }
