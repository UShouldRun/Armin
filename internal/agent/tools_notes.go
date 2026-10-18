package agent

// Tools on the Loop's own state: memories, the scratchpad, skills,
// and asking the user for read access outside the project.

import (
  "context"
  "encoding/json"
  "os"
  "path/filepath"
  "strings"

  "armin/internal/llm"
  "armin/internal/notes"
)

var noteTools = []toolSpec{
  {
    id:    toolMemorySave,
    flags: flagAgent | flagSession | flagMemories,
    run:   runMemorySave,
    def: toolFunc(
      "memory_save",
      "Save a durable fact for future sessions: user preferences, "+
        "project conventions, decisions and their reasons. Not "+
        "for task progress (use the scratchpad). Overwrites a "+
        "memory of the same name.",
      map[string]llm.ToolProperty{
        "name":        stringProp("Short kebab-case name"),
        "description": stringProp("One line — shown in the index every " +
          "session"),
        "content": stringProp(
          "The fact, plus why it matters and how to apply it",
        ),
        "scope": {
          Type:        "string",
          Description: "project (this codebase, default) or user (every " +
            "project)",
          Enum:        []string{ "project", "user" },
        },
      },
      "name", "description", "content",
    ),
  },
  {
    id:    toolMemoryRead,
    flags: flagAgent | flagSession | flagMemories,
    run:   runMemoryRead,
    def: toolFunc(
      "memory_read",
      "Read a memory listed in the index.",
      map[string]llm.ToolProperty{ "name": stringProp("Memory name") },
      "name",
    ),
  },
  {
    id:    toolMemoryDelete,
    flags: flagAgent | flagSession | flagMemories,
    run:   runMemoryDelete,
    def: toolFunc(
      "memory_delete",
      "Delete a memory that's wrong or obsolete.",
      map[string]llm.ToolProperty{ "name": stringProp("Memory name") },
      "name",
    ),
  },
  {
    id:    toolScratchpadWrite,
    flags: flagAgent | flagSession | flagScratch,
    run:   runScratchWrite,
    def: toolFunc(
      "scratchpad_write",
      "Write working notes to a scratch file outside the "+
        "project: plans, findings, long reasoning, checklists. "+
        "Keep only the file name in mind afterwards and re-read "+
        "it when needed — this frees context for the work.",
      map[string]llm.ToolProperty{
        "name":    stringProp("File name, e.g. plan or findings"),
        "content": stringProp("Markdown text"),
        "append": {
          Type:        "boolean",
          Description: "Append instead of replacing (default false)",
        },
      },
      "name", "content",
    ),
  },
  {
    id:    toolScratchpadRead,
    flags: flagAgent | flagSession | flagScratch,
    run:   runScratchRead,
    def: toolFunc(
      "scratchpad_read",
      "Read a scratch file, or a line range of it.",
      map[string]llm.ToolProperty{
        "name":       stringProp("File name"),
        "start_line": { Type: "integer", Description: "1-indexed, inclusive" },
        "end_line":   { Type: "integer", Description: "1-indexed, inclusive" },
      },
      "name",
    ),
  },
  {
    id:    toolScratchpadList,
    flags: flagAgent | flagSession | flagScratch,
    run:   runScratchList,
    def: toolFunc(
      "scratchpad_list",
      "List scratch files with their sizes.",
      nil,
    ),
  },
  {
    id:    toolRequestAccess,
    flags: flagAgent | flagSession,
    run:   runRequestAccess,
    def: toolFunc(
      "request_access",
      "Ask the user for read access to a directory outside the project "+
        "(e.g. slides or docs in the parent folder). The user approves "+
        "or declines; on approval, read tools accept absolute paths in "+
        "it. Read-only: writes stay inside the project.",
      map[string]llm.ToolProperty{
        "dir": stringProp("Directory: absolute, ~/..., or " +
          "relative to the project (\"..\")"),
        "reason": stringProp("One line: why you need it"),
      },
      "dir", "reason",
    ),
  },
  {
    id:    toolUseSkill,
    flags: flagAgent | flagSession | flagSkills,
    run:   runUseSkill,
    def: toolFunc(
      "use_skill",
      "Load a skill's instructions (see the skill index) before "+
        "doing a task it covers. With file, read one of its "+
        "supporting files instead.",
      map[string]llm.ToolProperty{
        "name": stringProp("Skill name"),
        "file": stringProp("Supporting file path, as listed by a previous " +
          "use_skill"),
      },
      "name",
    ),
  },
}

func runMemorySave(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[noteArgs](c)
  if bad != nil {
    return bad
  }

  scope, err := notes.ParseScope(in.Scope)
  if err != nil {
    return errResult(err)
  }

  mem, err := l.Memories.Save(in.Name, in.Description, in.Content, scope)
  if err != nil {
    return errResult(err)
  }

  return map[string]string{ "saved": mem.Name, "scope": mem.Scope.String() }
}

func runMemoryRead(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[noteArgs](c)
  if bad != nil {
    return bad
  }

  mem, body, err := l.Memories.Read(in.Name)
  if err != nil {
    return errResult(err)
  }

  return map[string]string{
    "name":        mem.Name,
    "scope":       mem.Scope.String(),
    "description": mem.Description,
    "content":     body,
  }
}

func runMemoryDelete(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[noteArgs](c)
  if bad != nil {
    return bad
  }

  mem, err := l.Memories.Delete(in.Name)
  if err != nil {
    return errResult(err)
  }

  return map[string]string{ "deleted": mem.Name }
}

func runScratchWrite(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[noteArgs](c)
  if bad != nil {
    return bad
  }

  f, err := l.Scratch.Write(in.Name, in.Content, in.Append)
  if err != nil {
    return errResult(err)
  }

  return f
}

func runScratchRead(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[noteArgs](c)
  if bad != nil {
    return bad
  }

  content, total, err := l.Scratch.Read(in.Name, in.StartLine, in.EndLine)
  if err != nil {
    return errResult(err)
  }

  return map[string]any{ "content": content, "total_lines": total }
}

func runScratchList(_ context.Context, l *Loop, _ toolCall) any {
  list, err := l.Scratch.List()
  if err != nil {
    return errResult(err)
  }

  return map[string]any{ "files": list }
}

func runUseSkill(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[noteArgs](c)
  if bad != nil {
    return bad
  }

  sk, ok := l.Skills.Get(in.Name)
  if !ok {
    return map[string]string{ "error": "no skill named " + in.Name }
  }

  if in.File != "" {
    content, truncated, err := sk.ReadFile(in.File)
    if err != nil {
      return errResult(err)
    }

    return map[string]any{ "content": content, "truncated": truncated }
  }

  body, err := sk.Instructions()
  if err != nil {
    return errResult(err)
  }

  return map[string]any{
    "skill":        sk.Name,
    "instructions": body,
    "files":        sk.Files(),
  }
}

func runRequestAccess(_ context.Context, l *Loop, c toolCall) any {
  return l.requestAccess(c.raw, c.hooks)
}

// requestAccess asks the user to let the model read a directory
// outside the project. With no one to ask (no Approve hook) it says
// how the user can grant it instead.
func (l *Loop) requestAccess(raw []byte, hooks Hooks) any {
  var in struct {
    Dir, Reason string
  }

  err := json.Unmarshal(raw, &in)
  if err != nil {
    return malformed("request_access", err)
  }

  dir := strings.TrimSpace(in.Dir)
  switch {
  case dir == "":
    return map[string]string{ "error": "dir is required" }
  case strings.HasPrefix(dir, "~/"):
    home, err := os.UserHomeDir()
    if err == nil {
      dir = filepath.Join(home, dir[2:])
    }
  case !filepath.IsAbs(dir):
    dir = filepath.Join(l.Sandbox.Root, dir)
  }

  dir = filepath.Clean(dir)
  info, err := os.Stat(dir)
  if err != nil || !info.IsDir() {
    return map[string]string{ "error": dir + " is not a directory" }
  }

  if hooks.Approve == nil {
    return map[string]string{
      "error": "no one to ask here; tell the user to start armin " +
        "with -add-dir " + dir + " (or type /add-dir " + dir + " in the REPL)",
    }
  }

  ok, feedback := hooks.Approve(
    "request_access", "read access to " + dir + "\nreason: " + in.Reason,
  )
  if !ok {
    return declinedResult("read access to " + dir, feedback)
  }

  resolved, err := l.Sandbox.AddReadRoot(dir)
  if err != nil {
    return map[string]string{ "error": err.Error() }
  }

  var names []string
  entries, _ := os.ReadDir(resolved)
  for _, e := range entries {
    if strings.HasPrefix(e.Name(), ".") {
      continue
    }

    if e.IsDir() {
      names = append(names, e.Name() + "/")
    } else {
      names = append(names, e.Name())
    }

    if len(names) == 40 {
      break
    }
  }

  return map[string]any{
    "granted":  resolved,
    "contents": names,
    "note":     "read tools accept absolute paths under " + resolved + " " +
      "now (e.g. " + filepath.Join(resolved, "<file>") + ")",
  }
}
