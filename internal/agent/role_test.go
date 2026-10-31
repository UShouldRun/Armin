package agent

import (
  "context"
  "strings"
  "testing"

  "armin/internal/llm"
)

func toolNames(l *Loop) map[string]bool {
  m := map[string]bool{}
  for _, d := range l.Definitions() {
    m[d.Function.Name] = true
  }

  return m
}

func TestRoleToolMasks(t *testing.T) {
  l := &Loop{}
  all := toolNames(l)

  l.SetRole(RoleAdvisor)
  adv := toolNames(l)
  for _, name := range []string{ "edit_file", "write_file", "run_program" } {
    if adv[name] {
      t.Errorf("the advisor is offered %s", name)
    }
  }

  if !adv["read_file"] || !adv["web_search"] || !adv["run_tests"] {
    t.Error("the advisor lost a read tool")
  }

  l.SetRole(RoleMinion)
  min := toolNames(l)
  if min["run_program"] || !min["edit_file"] {
    t.Errorf("minion: run_program %v, edit_file %v", min["run_program"], min["edit_file"])
  }

  l.SetRole(RoleCopilot)
  if len(toolNames(l)) != len(all) {
    t.Error("the copilot should have every tool")
  }
}

// TestRoleRefusesAtExecute: a model that calls a tool its role isn't
// offered (a stale definition, a hallucinated call) gets an error it
// can read, and nothing runs.
func TestRoleRefusesAtExecute(t *testing.T) {
  l := &Loop{}
  l.SetRole(RoleAdvisor)
  call := llm.ToolCall{}
  call.Function.Name = "write_file"
  call.Function.Arguments = map[string]any{ "path": "x", "content": "y" }
  res, ok := l.execute(context.Background(), call, Hooks{}).(map[string]string)
  if !ok || !strings.Contains(res["error"], "advisor role can't use write_file") {
    t.Fatalf("result = %v", res)
  }
}

func TestRoleReminder(t *testing.T) {
  l := &Loop{}
  history := []llm.Message{
    { Role: "system", Content: "sys" },
    { Role: "user", Content: "hi" },
    { Role: "assistant", Content: "hello" },
    { Role: "user", Content: "teach me tcp" },
  }

  l.SetRole(RoleAdvisor)
  l.takeReminder(history)
  got := history[3].Content
  if !strings.HasPrefix(got, "[armin] Your role is now advisor") ||
    !strings.HasSuffix(got, "\n\nteach me tcp") {
    t.Fatalf("reminder: %q", got)
  }

  if history[0].Content != "sys" || history[1].Content != "hi" {
    t.Fatal("earlier messages must not change")
  }

  l.takeReminder(history)
  if strings.Count(history[3].Content, "[armin]") != 1 {
    t.Fatal("the reminder goes in once")
  }

  l.SetRole(RoleAdvisor) // same role: nothing to remind
  if l.reminder != "" {
    t.Fatal("re-setting the same role queued a reminder")
  }
}

func TestParseRole(t *testing.T) {
  for _, r := range []Role{ RoleMinion, RoleCopilot, RoleAdvisor } {
    got, ok := ParseRole(r.String())
    if !ok || got != r {
      t.Errorf("ParseRole(%q) = %v, %v", r, got, ok)
    }
  }

  _, ok := ParseRole("planner")
  if ok {
    t.Error("unknown role parsed")
  }
}
