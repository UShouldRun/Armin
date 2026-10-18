// Note commands: /memory, /remember, /skills, /skill (and /<name>)
// and /scratch, on the stores internal/notes keeps.

package repl

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/ui"
)

func cmdMemory(r *REPL, _ context.Context, args []string) {
  mem := r.Loop.Memories
  if mem == nil {
    ui.Error("memory is unavailable this session")
    return
  }

  if len(args) >= 2 {
    switch args[0] {
    case "show":
      m, body, err := mem.Read(args[1])
      if err != nil {
        ui.Error("%s", err)
        return
      }

      fmt.Printf("%s %s\n", ui.Bold(m.Name), ui.Dim("(" + m.Scope.String() + ") " + m.Description))
      fmt.Println(strings.TrimSpace(body))

      return
    case "rm", "delete", "forget":
      m, err := mem.Delete(args[1])
      if err != nil {
        ui.Error("%s", err)
        return
      }

      ui.Info("forgot %s", m.Name)

      return
    }
  }

  list, err := mem.List()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if len(list) == 0 {
    ui.Info("no memories yet — /remember <text>, or the model saves its own")
    return
  }

  fmt.Println(ui.Bold("memories"))
  for _, m := range list {
    fmt.Printf("  %-24s %s\n", m.Name, ui.Dim("(" + m.Scope.String() + ") " + m.Description))
  }
}

func cmdRemember(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    ui.Error("usage: /remember <text>")
    return
  }

  prompt := "Save this to memory with memory_save (pick the scope: " +
    "user for my preferences, project for this codebase): " +
    strings.Join(args, " ")
  err := r.turn(ctx, prompt)
  if err != nil {
    ui.Error("%s", err)
  }

  r.maybeAutosave()
}

func cmdSkills(r *REPL, _ context.Context, _ []string) {
  if r.Loop.Skills == nil || r.Loop.Skills.Len() == 0 {
    ui.Info(
      "no skills — add <name>/SKILL.md under ~/.config/armin/skills " +
        "or .armin/skills in the project",
    )

    return
  }

  fmt.Println(ui.Bold("skills"))
  for _, sk := range r.Loop.Skills.List() {
    where := "user"
    if sk.Project {
      where = "project"
    }

    fmt.Printf("  %-20s %s\n", sk.Name, ui.Dim("(" + where + ") " + sk.Description))
  }
}

// cmdSkill puts the skill's instructions straight into the user
// message, so the model needn't spend a round on use_skill.
func cmdSkill(r *REPL, ctx context.Context, args []string) {
  if len(args) == 0 {
    cmdSkills(r, ctx, nil)
    return
  }

  if r.Loop.Skills == nil {
    ui.Error("no skills loaded")
    return
  }

  sk, ok := r.Loop.Skills.Get(args[0])
  if !ok {
    ui.Error("no skill named %q — /skills lists them", args[0])
    return
  }

  body, err := sk.Instructions()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  task := strings.Join(args[1:], " ")
  if task == "" {
    task = "(no extra input — follow the skill as written)"
  }

  prompt := fmt.Sprintf(
    "Use the %q skill for this task: %s\n\n<skill name=%q>\n%s\n</skill>",
    sk.Name, task, sk.Name, strings.TrimSpace(body),
  )
  files := sk.Files()
  if len(files) > 0 {
    prompt += "\nSupporting files (use_skill with file): " +
      strings.Join(files, ", ")
  }

  err = r.turn(ctx, prompt)
  if err != nil {
    ui.Error("%s", err)
  }

  r.maybeAutosave()
}

func cmdScratch(r *REPL, _ context.Context, args []string) {
  pad := r.Loop.Scratch
  if pad == nil {
    ui.Error("the scratchpad is unavailable this session")
    return
  }

  if len(args) >= 1 && args[0] == "clear" {
    n, err := pad.Clear()
    if err != nil {
      ui.Error("%s", err)
      return
    }

    ui.Info("cleared %d scratch files", n)

    return
  }

  if len(args) >= 2 && args[0] == "show" {
    content, _, err := pad.Read(args[1], 0, 0)
    if err != nil {
      ui.Error("%s", err)
      return
    }

    fmt.Print(content)

    return
  }

  list, err := pad.List()
  if err != nil {
    ui.Error("%s", err)
    return
  }

  if len(list) == 0 {
    ui.Info("scratchpad is empty (%s)", pad.Dir)
    return
  }

  fmt.Println(ui.Bold("scratchpad") + ui.Dim(" " + pad.Dir))
  for _, f := range list {
    fmt.Printf(
      "  %-24s %s\n", f.Name,
      ui.Dim(fmt.Sprintf("%d lines, %s", f.Lines, f.Modified.Format("Jan 2 15:04"))),
    )
  }
}
