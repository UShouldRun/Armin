package repl

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/agent"
  "armin/internal/ui"
)

// SetEffort sets how hard the model thinks (-effort, effort =).
func (r *REPL) SetEffort(level agent.Effort) {
  r.effort = level
}

// turnPlan is this turn's effort and kind of task: a one-off level
// from /effort, else the session's. The model is asked when effort is
// auto, or when procedures need the kind; armin shows what it chose.
func (r *REPL) turnPlan(
  ctx context.Context, input string,
) (agent.Effort, agent.Task) {
  effort := r.effort
  if r.once != agent.EffortDefault {
    effort, r.once = r.once, agent.EffortDefault
  }

  auto := effort == agent.EffortAuto
  if !auto && !r.Loop.Aids.Has(agent.AidProcedure) {
    return effort, agent.TaskOther
  }

  judged, task := agent.Assess(ctx, r.LLM, input)
  if auto {
    effort = judged
    fmt.Println(ui.Dim("thinking: " + effort.String() + " (auto), " +
      task.String() + " task"))
  }

  return effort, task
}

// cmdEffort is /effort [level] [task]: show or set how hard the
// model thinks; with a task, that level is for the task alone.
func cmdEffort(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    usage := "(/effort " + agent.EffortNames + " [task])"
    fmt.Printf("effort: %s %s\n", r.effort, ui.Dim(usage))

    return
  }

  level, ok := agent.ParseEffort(strings.ToLower(args[0]))
  if !ok {
    ui.Error("effort %q: want one of %s", args[0], agent.EffortNames)
    return
  }

  if len(args) == 1 {
    r.effort = level
    fmt.Println(ui.Dim("effort: " + level.String()))

    return
  }

  r.once = level
  err := r.turn(ctx, strings.Join(args[1:], " "))
  if err != nil {
    ui.Error("%s", err)
  }

  r.maybeAutosave()
}
