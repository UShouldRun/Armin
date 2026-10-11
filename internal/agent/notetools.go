package agent

import (
  "encoding/json"

  "armin/internal/llm"
  "armin/internal/notes"
)

func stringProp(description string) llm.ToolProperty {
  return llm.ToolProperty{ Type: "string", Description: description }
}

func noteDef(
  name, description string,
  props map[string]llm.ToolProperty, required ...string,
) llm.ToolDefinition {
  if props == nil {
    props = map[string]llm.ToolProperty{}
  }

  return llm.ToolDefinition{
    Type: "function",
    Function: llm.ToolFunction{
      Name:        name,
      Description: description,
      Parameters: llm.ToolParameters{
        Type:       "object",
        Properties: props,
        Required:   required,
      },
    },
  }
}

// noteDefinitions returns the memory/scratchpad/skill tools for the
// stores this Loop has.
func (l *Loop) noteDefinitions() []llm.ToolDefinition {
  var defs []llm.ToolDefinition

  if l.Memories != nil {
    defs = append(defs,
      noteDef(
        "memory_save",
        "Save a durable fact for future sessions: user preferences, "+
          "project conventions, decisions and their reasons. Not "+
          "for task progress (use the scratchpad). Overwrites a "+
          "memory of the same name.",
        map[string]llm.ToolProperty{
          "name":        stringProp("Short kebab-case name"),
          "description": stringProp("One line — shown in the index every session"),
          "content": stringProp(
            "The fact, plus why it matters and how to apply it",
          ),
          "scope": {
            Type:        "string",
            Description: "project (this codebase, default) or user (every project)",
            Enum:        []string{ "project", "user" },
          },
        },
        "name", "description", "content",
      ),
      noteDef(
        "memory_read",
        "Read a memory listed in the index.",
        map[string]llm.ToolProperty{ "name": stringProp("Memory name") },
        "name",
      ),
      noteDef(
        "memory_delete",
        "Delete a memory that's wrong or obsolete.",
        map[string]llm.ToolProperty{ "name": stringProp("Memory name") },
        "name",
      ),
    )
  }

  if l.Scratch != nil {
    defs = append(defs,
      noteDef(
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
      noteDef(
        "scratchpad_read",
        "Read a scratch file, or a line range of it.",
        map[string]llm.ToolProperty{
          "name":       stringProp("File name"),
          "start_line": { Type: "integer", Description: "1-indexed, inclusive" },
          "end_line":   { Type: "integer", Description: "1-indexed, inclusive" },
        },
        "name",
      ),
      noteDef(
        "scratchpad_list",
        "List scratch files with their sizes.",
        nil,
      ),
    )
  }

  if l.Skills != nil && l.Skills.Len() > 0 {
    defs = append(defs, noteDef(
      "use_skill",
      "Load a skill's instructions (see the skill index) before "+
        "doing a task it covers. With file, read one of its "+
        "supporting files instead.",
      map[string]llm.ToolProperty{
        "name": stringProp("Skill name"),
        "file": stringProp("Supporting file path, as listed by a previous use_skill"),
      },
      "name",
    ))
  }

  return defs
}

func errResult(err error) map[string]string {
  return map[string]string{ "error": err.Error() }
}

// executeNote runs a note tool; ok is false for any other tool.
func (l *Loop) executeNote(id ToolID, raw []byte) (any, bool) {
  switch id {
  case toolMemorySave, toolMemoryRead, toolMemoryDelete:
    if l.Memories == nil {
      return nil, false
    }

    return l.executeMemory(id, raw), true

  case toolScratchpadWrite, toolScratchpadRead, toolScratchpadList:
    if l.Scratch == nil {
      return nil, false
    }

    return l.executeScratch(id, raw), true

  case toolUseSkill:
    if l.Skills == nil {
      return nil, false
    }

    return l.executeSkill(raw), true
  }

  return nil, false
}

func (l *Loop) executeMemory(id ToolID, raw []byte) any {
  var in struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Content     string `json:"content"`
    Scope       string `json:"scope"`
  }

  if err := json.Unmarshal(raw, &in); err != nil {
    return malformed("memory tool", err)
  }

  switch id {
  case toolMemorySave:
    scope, err := notes.ParseScope(in.Scope)
    if err != nil {
      return errResult(err)
    }

    mem, err := l.Memories.Save(in.Name, in.Description, in.Content, scope)
    if err != nil {
      return errResult(err)
    }

    return map[string]string{ "saved": mem.Name, "scope": mem.Scope.String() }

  case toolMemoryRead:
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

  default:
    mem, err := l.Memories.Delete(in.Name)
    if err != nil {
      return errResult(err)
    }

    return map[string]string{ "deleted": mem.Name }
  }
}

func (l *Loop) executeScratch(id ToolID, raw []byte) any {
  var in struct {
    Name      string `json:"name"`
    Content   string `json:"content"`
    Append    bool   `json:"append"`
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
  }

  if err := json.Unmarshal(raw, &in); err != nil {
    return malformed("scratchpad tool", err)
  }

  switch id {
  case toolScratchpadWrite:
    f, err := l.Scratch.Write(in.Name, in.Content, in.Append)
    if err != nil {
      return errResult(err)
    }

    return f

  case toolScratchpadRead:
    content, total, err := l.Scratch.Read(in.Name, in.StartLine, in.EndLine)
    if err != nil {
      return errResult(err)
    }

    return map[string]any{ "content": content, "total_lines": total }

  default:
    list, err := l.Scratch.List()
    if err != nil {
      return errResult(err)
    }

    return map[string]any{ "files": list }
  }
}

func (l *Loop) executeSkill(raw []byte) any {
  var in struct {
    Name string `json:"name"`
    File string `json:"file"`
  }

  if err := json.Unmarshal(raw, &in); err != nil {
    return malformed("use_skill", err)
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
