// Roles: the minion (a small local model: searches, reads, small
// edits), the copilot (the full agent) and the advisor (decisions and
// teaching; reads but doesn't write). A role decides which tools the
// model is offered and may call; switching appends a short reminder
// to the next user message instead of rewriting the system prompt, so
// everything before it stays in the server's prompt cache.
//
//   SetRole      switch, queueing the reminder
//   ParseRole    "minion" | "copilot" | "advisor"
//   allows       whether a tool's spec admits the current role

package agent

import (
  "armin/internal/llm"
)

var roleNames = [...]struct {
  role Role
  name string
}{
  { RoleMinion, "minion" },
  { RoleCopilot, "copilot" },
  { RoleAdvisor, "advisor" },
}

// roleReminders are what the model reads when its role changes.
var roleReminders = map[Role]string{
  RoleMinion: "[armin] Your role is now minion: a small local model. " +
    "Do searches, reads and small, contained edits, one thing at a " +
    "time, and check each result. Say plainly when a task is too " +
    "big or unclear for you.",
  RoleCopilot: "[armin] Your role is now copilot: the full agent, for " +
    "large searches, reasoning and changes across the codebase, with " +
    "every tool.",
  RoleAdvisor: "[armin] Your role is now advisor: help the user make " +
    "key decisions and learn. You can read the code, the web and the " +
    "library, but not change files; when code needs writing, say " +
    "what, and leave it to the copilot.",
}

func (r Role) String() string {
  for _, n := range roleNames {
    if n.role == r {
      return n.name
    }
  }

  if r == 0 {
    return "none"
  }

  return "?"
}

// ParseRole reads a role's name.
func ParseRole(s string) (Role, bool) {
  for _, n := range roleNames {
    if n.name == s {
      return n.role, true
    }
  }

  return 0, false
}

// slot is the role's index into Loop.roleDefs: 0 for no role.
func (r Role) slot() int {
  switch r {
  case RoleMinion:
    return 1
  case RoleCopilot:
    return 2
  case RoleAdvisor:
    return 3
  }

  return 0
}

// allows reports whether a tool admits role r: a spec without roles
// admits every role, and no role admits every tool.
func (id ToolID) allows(r Role) bool {
  roles := specs[id].roles

  return r == 0 || roles == 0 || roles & r != 0
}

// SetRole switches the Loop's role; the reminder goes in front of the
// next user message (see takeReminder). Call it between turns.
func (l *Loop) SetRole(r Role) {
  if r == l.Role {
    return
  }

  l.Role = r
  l.reminder = roleReminders[r]
}

// takeReminder puts a pending role reminder in front of the user
// message that starts this turn. That message is new, so nothing
// cached changes.
func (l *Loop) takeReminder(history []llm.Message) {
  if l.reminder == "" || len(history) == 0 {
    return
  }

  last := &history[len(history) - 1]
  if last.Role != "user" {
    return
  }

  last.Content = l.reminder + "\n\n" + last.Content
  l.reminder = ""
}
