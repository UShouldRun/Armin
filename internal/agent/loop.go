// Package agent implements the tool-calling round trip: send
// messages to the model, execute any tool calls it asks for, feed
// results back, and repeat until it answers in plain text.
package agent

import (
  "context"
  "encoding/json"
  "fmt"

  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/tools"
)

// maxToolRounds bounds how many tool-call round trips a single user
// turn can take, so a confused model can't loop forever burning
// tokens.
const maxToolRounds = 8

// Loop wires an LLM client to the sandboxed tool implementations.
type Loop struct {
  LLM     *llm.Client
  Sandbox *sandbox.Config

  // OnToolCall, when set, is called just before each tool call
  // executes — the REPL uses it to show what the model is doing in
  // real time.
  OnToolCall func(name string, args map[string]any)
}

// New returns a ready-to-use Loop.
func New(client *llm.Client, sb *sandbox.Config) *Loop {
  return &Loop{ LLM: client, Sandbox: sb }
}

// ToolDefinitions returns the schema for all four tools, sent to
// Ollama alongside every chat request. Descriptions are kept short
// and concrete per plan section 4 — small models are sensitive to
// prompt bloat.
func ToolDefinitions() []llm.ToolDefinition {
  return []llm.ToolDefinition{
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "search_code",
        Description: "Search the codebase for a pattern using " +
          "ripgrep. Returns matched lines with a couple lines of " +
          "context, not full files.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "pattern": {
              Type:        "string",
              Description: "Regex or literal pattern to search for",
            },
            "path": {
              Type:        "string",
              Description: "Directory or file to search under (default: project root)",
            },
            "file_type": {
              Type:        "string",
              Description: "Restrict to a file type, e.g. go, py, js",
            },
          },
          Required: []string{ "pattern" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
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
      Type: "function",
      Function: llm.ToolFunction{
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
      Type: "function",
      Function: llm.ToolFunction{
        Name: "run_command",
        Description: "Run an allowlisted, read-only binary " +
          "directly (no shell). args is always an array of " +
          "separate arguments, never one shell string.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "binary": {
              Type:        "string",
              Description: "Bare binary name, e.g. git, wc, find",
            },
            "args": {
              Type:        "array",
              Description: `Arguments as separate array entries, e.g. ["log", "--oneline", "-5"]`,
            },
          },
          Required: []string{ "binary" },
        },
      },
    },
  }
}

// Turn runs one user turn to completion: it sends history, executes
// any tool calls the model makes, feeds results back, and returns
// once the model responds with plain text (or the round limit is
// hit). history is mutated in place so callers can persist it
// across turns. onToken, if non-nil, receives content deltas as the
// model streams its reply.
func (l *Loop) Turn(
  ctx context.Context, history *[]llm.Message, onToken llm.OnToken,
) (string, error) {
  defs := ToolDefinitions()

  for round := 0; round < maxToolRounds; round++ {
    reply, err := l.LLM.Chat(ctx, *history, defs, onToken)
    if err != nil {
      return "", fmt.Errorf(
        "\n\t... in Turn:\n\t model call failed: %w", err,
      )
    }

    *history = append(*history, *reply)

    if len(reply.ToolCalls) == 0 {
      return reply.Content, nil
    }

    for _, call := range reply.ToolCalls {
      if l.OnToolCall != nil {
        l.OnToolCall(call.Function.Name, call.Function.Arguments)
      }

      result := l.execute(ctx, call)
      resultJSON, err := json.Marshal(result)
      if err != nil {
        resultJSON = []byte(fmt.Sprintf(
          `{"error":"failed to encode tool result: %s"}`, err,
        ))
      }

      *history = append(*history, llm.Message{
        Role:     "tool",
        ToolName: call.Function.Name,
        Content:  string(resultJSON),
      })
    }
  }

  return "", fmt.Errorf(
    "\n\t... in Turn:\n\t hit max tool rounds (%d) without a "+
      "final answer — the model may be looping",
    maxToolRounds,
  )
}

// execute dispatches one tool call to its implementation and
// returns a JSON-marshalable result. Errors are returned as
// structured data in the result itself (see plan section 6), not as
// Go errors, so a bad call becomes something the model can read and
// self-correct from on the next round rather than a crash.
func (l *Loop) execute(ctx context.Context, call llm.ToolCall) any {
  name := call.Function.Name

  raw, err := json.Marshal(call.Function.Arguments)
  if err != nil {
    return map[string]string{
      "error": fmt.Sprintf("could not encode arguments: %s", err),
    }
  }

  switch name {
  case "search_code":
    var in tools.SearchCodeInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.SearchCode(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "read_file":
    var in tools.ReadFileInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ReadFile(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "list_directory":
    var in tools.ListDirectoryInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ListDirectory(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "run_command":
    var in tools.RunCommandInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, cmdErr := tools.RunCommand(ctx, l.Sandbox, in)
    if cmdErr != nil {
      return cmdErr
    }
    return out

  default:
    return map[string]string{ "error": fmt.Sprintf("unknown tool %q", name) }
  }
}

func malformed(toolName string, err error) map[string]string {
  return map[string]string{
    "error": fmt.Sprintf(
      "malformed arguments for %s: %s — check the parameter "+
        "names and types against the tool schema and try again",
      toolName, err,
    ),
  }
}
