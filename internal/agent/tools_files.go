package agent

// Tools that read, list and change files.

import (
  "context"
  "strings"

  "armin/internal/llm"
  "armin/internal/tools"
)

var fileTools = []toolSpec{
  {
    id:    toolReadFile,
    run:   runReadFile,
    paths: pathArg,
    def: llm.ToolFunction{
      Name: "read_file",
      Description: "Read a line range from a file. Files over " +
        "400 lines require start_line/end_line — search first " +
        "to find the range you need.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "File path, relative to project root",
          },
          "start_line": {
            Type:        "integer",
            Description: "1-indexed start line (inclusive)",
          },
          "end_line": {
            Type:        "integer",
            Description: "1-indexed end line (inclusive)",
          },
        },
        Required: []string{ "path" },
      },
    },
  },
  {
    id:    toolReadPDF,
    run:   inSandbox(tools.ReadPDF),
    paths: pathArg,
    def: llm.ToolFunction{
      Name: "read_pdf",
      Description: "Extract the text of a PDF (slides, papers, " +
        "manuals), a page range at a time, with --- page N --- " +
        "markers. Default: 10 pages from first_page.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "PDF path, relative to the project or absolute in " +
              "a directory the user added",
          },
          "first_page": {
            Type:        "integer",
            Description: "1-indexed first page (default 1)",
          },
          "last_page": {
            Type:        "integer",
            Description: "Last page (default first_page + 9)",
          },
        },
        Required: []string{ "path" },
      },
    },
  },
  {
    id:    toolListDirectory,
    run:   inSandbox(tools.ListDirectory),
    paths: pathArg,
    def: llm.ToolFunction{
      Name: "list_directory",
      Description: "List a directory as an indented tree, up to " +
        "a given depth. Skips .git, node_modules, vendor, and " +
        "build output.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "Directory to list (default: project root)",
          },
          "depth": {
            Type:        "integer",
            Description: "How many levels deep to recurse (default: 2)",
          },
        },
      },
    },
  },
  {
    id:    toolEditFile,
    flags: flagWrite,
    run:   runEditFile,
    def: llm.ToolFunction{
      Name: "edit_file",
      Description: "Replace an exact string in a file. Read the " +
        "file first. old_string must match exactly once, " +
        "whitespace included — add surrounding lines to make it " +
        "unique, or set replace_all. Prefer this over write_file " +
        "for changes to existing files.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "File path, relative to project root",
          },
          "old_string": {
            Type:        "string",
            Description: "Exact text to replace, copied from read_file",
          },
          "new_string": {
            Type:        "string",
            Description: "Replacement text (empty deletes old_string)",
          },
          "replace_all": {
            Type:        "boolean",
            Description: "Replace every occurrence (default false)",
          },
        },
        Required: []string{ "path", "old_string", "new_string" },
      },
    },
  },
  {
    id:    toolWriteFile,
    flags: flagWrite,
    run:   runWriteFile,
    def: llm.ToolFunction{
      Name: "write_file",
      Description: "Create a file, or overwrite one you have " +
        "already read. Parent directories are created. Use " +
        "edit_file for partial changes.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "File path, relative to project root",
          },
          "content": {
            Type:        "string",
            Description: "The complete file content",
          },
        },
        Required: []string{ "path", "content" },
      },
    },
  },
  {
    id:    toolCountLines,
    run:   inSandbox(tools.CountLines),
    def: llm.ToolFunction{
      Name: "count_lines",
      Description: "Count lines of code across the project (or a " +
        "subdirectory) in one call — total, plus a breakdown by " +
        "top-level directory.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "Directory to count under (default: project root)",
          },
        },
      },
    },
  },
  {
    id:    toolFindFiles,
    run:   inSandbox(tools.FindFiles),
    def: llm.ToolFunction{
      Name: "find_files",
      Description: "Find files by name using a glob pattern " +
        "(e.g. \"*.go\", \"*_test.go\") — a name-only complement " +
        "to search_code's content search.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "pattern": {
            Type:        "string",
            Description: "Glob matched against each file's base name",
          },
          "path": {
            Type:        "string",
            Description: "Directory to search under (default: project root)",
          },
        },
        Required: []string{ "pattern" },
      },
    },
  },
  {
    id:    toolFileStat,
    run:   inSandbox(tools.FileStat),
    def: llm.ToolFunction{
      Name: "file_stat",
      Description: "Get a file's size, line count, and " +
        "modification time without reading its content — cheaper " +
        "than read_file for deciding what to read.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "File path, relative to project root",
          },
        },
        Required: []string{ "path" },
      },
    },
  },
}

// runReadFile reads, and marks the file as seen: write_file and
// edit_file refuse files the model hasn't read (or that changed).
func runReadFile(ctx context.Context, l *Loop, c toolCall) any {
  in, bad := decode[tools.ReadFileInput](c)
  if bad != nil {
    return bad
  }

  out, err := tools.ReadFile(ctx, l.Sandbox, in)
  if err != nil {
    return errResult(err)
  }

  resolved, err := l.Sandbox.ResolvePath(in.Path)
  if err == nil {
    l.Tracker.Mark(resolved)
  }

  return out
}

func runWriteFile(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[tools.WriteFileInput](c)
  if bad != nil {
    return bad
  }

  change, err := tools.PlanWrite(l.Sandbox, l.Tracker, in)
  if err != nil {
    return errResult(err)
  }

  return l.applyChange(c.name, change, c.hooks)
}

func runEditFile(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[tools.EditFileInput](c)
  if bad != nil {
    return bad
  }

  change, err := tools.PlanEdit(l.Sandbox, l.Tracker, in)
  if err != nil {
    return errResult(err)
  }

  return l.applyChange(c.name, change, c.hooks)
}

// applyChange asks hooks.Approve (if set) before writing.
func (l *Loop) applyChange(
  name string, change *tools.Change, hooks Hooks,
) any {
  if hooks.Propose != nil {
    start, gone, came, oldLines, newLines := change.Hunk()
    hooks.Propose(Proposal{
      Tool: name, Path: change.Path, Resolved: change.Resolved,
      Old: change.Old, New: change.New, Created: change.Created,
      Start: start, Gone: gone, Came: came,
      OldLines: oldLines, NewLines: newLines,
    })
  }

  if hooks.Approve != nil {
    ok, feedback := hooks.Approve(name, change.Preview())
    if !ok {
      return declinedResult("this change (nothing was written)", feedback)
    }
  }

  out, err := change.Apply(l.Sandbox, l.Tracker)
  if err != nil {
    return map[string]string{ "error": err.Error() }
  }

  return out
}

// declinedResult tells the model the user said no. Their words go
// in the error itself, not a side field: small models read the
// error and skip the rest.
func declinedResult(what, feedback string) map[string]string {
  msg := "the user declined " + what + "."
  feedback = strings.TrimSpace(feedback)
  if feedback != "" {
    msg += " The user said: \"" + feedback + "\". Do what they said " +
      "instead of retrying the same call; if it isn't clear, stop and ask them."
  } else {
    msg += " Don't retry it: change approach, or stop and ask the user."
  }

  return map[string]string{ "error": msg }
}
