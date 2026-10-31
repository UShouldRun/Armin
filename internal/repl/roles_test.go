package repl

import (
	"context"
	"testing"

	"armin/internal/agent"
	"armin/internal/llamacpp"
	"armin/internal/llm"
)

// TestSwitchRoleWithoutModel: a role with no model in config.conf
// switches the role and keeps the model.
func TestSwitchRoleWithoutModel(t *testing.T) {
	r := &REPL{
		Loop:   &agent.Loop{},
		LLM:    &llm.Client{Model: "local"},
		Models: llamacpp.New(map[string]llamacpp.ModelSpec{}),
	}
	r.switchRole(context.Background(), agent.RoleAdvisor)
	if r.Loop.Role != agent.RoleAdvisor || r.currentModel() != "local" {
		t.Fatalf("role %v, model %q", r.Loop.Role, r.currentModel())
	}

	for _, d := range r.Loop.Definitions() {
		if d.Function.Name == "edit_file" {
			t.Fatal("the advisor was offered edit_file")
		}
	}
}

func TestRoleCommandsDispatch(t *testing.T) {
	r := &REPL{
		Loop:   &agent.Loop{},
		LLM:    &llm.Client{Model: "local"},
		Models: llamacpp.New(map[string]llamacpp.ModelSpec{}),
	}
	for _, c := range []struct {
		line string
		role agent.Role
	}{
		{"/advisor", agent.RoleAdvisor},
		{"/minion", agent.RoleMinion},
		{"/copilot", agent.RoleCopilot},
	} {
		r.dispatch(context.Background(), c.line)
		if r.Loop.Role != c.role {
			t.Fatalf("%s: role %v", c.line, r.Loop.Role)
		}
	}
}
