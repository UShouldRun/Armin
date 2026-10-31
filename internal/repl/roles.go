// /minion, /copilot and /advisor: switch the role (which tools the
// model is offered, see agent.Role) and, when config.conf names a
// model for that role, the model too.

package repl

import (
  "context"

  "armin/internal/agent"
  "armin/internal/ui"
)

// cmdRole runs as whichever of the three commands was typed
// (dispatch records it in lastCommand).
func cmdRole(r *REPL, ctx context.Context, args []string) {
  role, ok := agent.ParseRole(r.lastCommand)
  if !ok {
    return
  }

  r.switchRole(ctx, role)
}

// switchRole moves to role's model, if it has one and it isn't the
// current one, then to the role itself.
func (r *REPL) switchRole(ctx context.Context, role agent.Role) {
  model := r.RoleModels[role]
  if model != "" && model != r.currentModel() {
    r.switchModel(ctx, model)
    if r.currentModel() != model {
      return // switchModel said why
    }
  }

  r.Loop.SetRole(role)
  if model == "" {
    ui.Info("now the %s, on %s (no %s = in config.conf)", role, r.modelName(), role)
    return
  }

  ui.Info("now the %s, on %s", role, model)
}

// currentModel is the configured name of the chat model, as /model
// and switchModel use it.
func (r *REPL) currentModel() string {
  cur := r.current.Load()
  if cur == nil {
    return r.LLM.Model
  }

  return *cur
}
