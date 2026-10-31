// The types of role.go.

package agent

import (
  "sync"

  "armin/internal/llm"
)

// Role is who armin is acting as. It is a bitmask so a tool spec can
// name every role that may call it; a Loop has one role at a time,
// or none (every tool, as before roles existed).
type Role uint8

const (
  RoleMinion  Role = 0b00000001
  RoleCopilot Role = 0b00000010
  RoleAdvisor Role = 0b00000100
  RoleAll     Role = 0b11111111
)

// roleDefs is one role's tool definitions, built on first use: the
// list sent with every request, each tool's parameter names (for
// unknownParams), and the list's rough size in tokens.
type roleDefs struct {
  once   sync.Once
  list   []llm.ToolDefinition
  params map[string]map[string]bool
  tokens int
}
