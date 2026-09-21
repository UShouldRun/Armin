// Package agent implements the tool-calling round trip: send
// messages to the model, execute any tool calls it asks for, feed
// results back, and repeat until it answers in plain text.
package agent

import (
  "context"
  "encoding/json"
  "fmt"

  "armin/internal/ctags"
  "armin/internal/llm"
  "armin/internal/sandbox"
  "armin/internal/searxng"
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

  // WebSearch backs the web_search tool. Nil disables it gracefully
  // — the tool is still advertised (see ToolDefinitions), but every
  // call returns a clean "unavailable" error instead of a panic.
  // cmd/armin/main.go only sets this when docker was found on PATH
  // at startup, the same degrade-gracefully pattern -ctags-bin uses.
  WebSearch *searxng.Manager

  // Symbols backs find_definition/list_symbols/outline_file/
  // find_dead_code/read_doc_comment — the same ctags index the LSP
  // server's real textDocument/definition and workspace/symbol use.
  // cmd/armin/main.go builds one shared *ctags.Index and wires it
  // into both this field and lspserver, so -lsp doesn't scan the
  // repo twice. Nil (no universal-ctags binary at startup) degrades
  // the same way WebSearch does.
  Symbols *ctags.Index
}

// New returns a ready-to-use Loop.
func New(client *llm.Client, sb *sandbox.Config) *Loop {
  return &Loop{ LLM: client, Sandbox: sb }
}

// ToolDefinitions returns the schema for all 34 tools, sent to the
// model alongside every chat request. Descriptions are kept short
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
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "web_search",
        Description: "Search the web for information not in this " +
          "codebase — library docs, error messages, current facts. " +
          "Starts a local search service on first use; returns a " +
          "short list of title/url/snippet results, not full pages.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "query": {
              Type:        "string",
              Description: "Search query",
            },
            "count": {
              Type:        "integer",
              Description: "Number of results to return (default 5, max 10)",
            },
          },
          Required: []string{ "query" },
        },
      },
    },

    // --- repo stats ---
    {
      Type: "function",
      Function: llm.ToolFunction{
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
      Type: "function",
      Function: llm.ToolFunction{
        Name: "list_languages",
        Description: "Break the project down by file extension — " +
          "file and line counts per language, ranked by lines.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Directory to scan (default: project root)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
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
      Type: "function",
      Function: llm.ToolFunction{
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
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "find_todo_fixme",
        Description: "Find TODO/FIXME/XXX comment markers in the " +
          "codebase.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Directory or file to search under (default: project root)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "env_info",
        Description: "Report the toolchain available: Go version, " +
          "OS/arch, and whether git/ripgrep/docker are installed.",
        Parameters: llm.ToolParameters{ Type: "object", Properties: map[string]llm.ToolProperty{} },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "project_manifest",
        Description: "Get an orientation snapshot of the project: " +
          "directory tree plus the first README/manifest file found. " +
          "Useful for \"what kind of project is this\" mid-conversation.",
        Parameters: llm.ToolParameters{ Type: "object", Properties: map[string]llm.ToolProperty{} },
      },
    },

    // --- symbol navigation (ctags-backed) ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "find_definition",
        Description: "Find where a function, type, or variable is " +
          "defined — a deterministic index lookup, not a guess.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "name": {
              Type:        "string",
              Description: "Symbol name to look up",
            },
            "from_file": {
              Type:        "string",
              Description: "File you're currently looking at, to rank same-file/same-dir matches first",
            },
          },
          Required: []string{ "name" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "list_symbols",
        Description: "Fuzzy-search every known symbol name in the " +
          "project (functions, types, variables, ...).",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "query": {
              Type:        "string",
              Description: "Substring to match against symbol names (empty lists everything, capped)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "outline_file",
        Description: "List a file's known symbols without reading " +
          "its content — a cheap first look at a large file before " +
          "deciding what range to read.",
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
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "find_references",
        Description: "Find where an identifier is USED (not where " +
          "it's defined — see find_definition), word-boundary matched.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "name": {
              Type:        "string",
              Description: "Identifier to search for",
            },
            "path": {
              Type:        "string",
              Description: "Directory or file to search under (default: project root)",
            },
          },
          Required: []string{ "name" },
        },
      },
    },

    // --- git history ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "git_log",
        Description: "Get structured commit history (hash, author, " +
          "date, subject) for the project or one path.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Limit history to this path (default: whole repo)",
            },
            "count": {
              Type:        "integer",
              Description: "Commits to return (default 20, max 100)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "git_blame",
        Description: "Get per-line author/date attribution for a " +
          "line range in a file.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "File path, relative to project root",
            },
            "start_line": {
              Type:        "integer",
              Description: "1-indexed start line (default 1)",
            },
            "end_line": {
              Type:        "integer",
              Description: "1-indexed end line (default start_line+199)",
            },
          },
          Required: []string{ "path" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "git_diff",
        Description: "Diff between two refs (default: HEAD vs. the " +
          "working tree) for a path or the whole repo.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Limit the diff to this path (default: whole repo)",
            },
            "from": {
              Type:        "string",
              Description: "Starting ref (default: HEAD)",
            },
            "to": {
              Type:        "string",
              Description: "Ending ref (default: working tree)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "git_show",
        Description: "Get one commit's full diff and message by hash.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "hash": {
              Type:        "string",
              Description: "Commit hash (short or full)",
            },
          },
          Required: []string{ "hash" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "working_tree_diff",
        Description: "Show uncommitted changes only — what you've " +
          "edited but not committed, plus untracked files.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Limit to this path (default: whole repo)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "grep_history",
        Description: "Find when a piece of text was introduced or " +
          "removed in git history (a \"pickaxe\" search).",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "term": {
              Type:        "string",
              Description: "Text to search for across commit history",
            },
            "path": {
              Type:        "string",
              Description: "Limit to this path (default: whole repo)",
            },
            "count": {
              Type:        "integer",
              Description: "Commits to return (default 10, max 50)",
            },
          },
          Required: []string{ "term" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "code_owners",
        Description: "Approximate who owns a path, by commit count " +
          "per author — a heuristic, not an authoritative CODEOWNERS lookup.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Path to check (default: whole repo)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "commit_stats",
        Description: "Rank files by how often they've changed — a " +
          "churn/hotspot signal for what's worth extra scrutiny.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Limit to this path (default: whole repo)",
            },
            "count": {
              Type:        "integer",
              Description: "Top-N files to return (default 10, max 50)",
            },
          },
        },
      },
    },

    // --- build/test/lint ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "run_tests",
        Description: "Run `go test` and report pass/fail plus a " +
          "summary, instead of raw test output.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "package": {
              Type:        "string",
              Description: "Package path to test (default: ./...)",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "build_check",
        Description: "Run `go build` (or `go vet`) and report " +
          "pass/fail plus any errors.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "package": {
              Type:        "string",
              Description: "Package path to build (default: ./...)",
            },
            "vet": {
              Type:        "boolean",
              Description: "Run go vet instead of go build",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "lint_check",
        Description: "Run golangci-lint if it's installed; reports " +
          "unavailable rather than erroring if it isn't.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "package": {
              Type:        "string",
              Description: "Package path to lint (default: ./...)",
            },
          },
        },
      },
    },

    // --- dependencies ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "list_dependencies",
        Description: "List the project's declared dependencies, " +
          "parsed from whichever manifest exists (go.mod, " +
          "package.json, requirements.txt, Cargo.toml, pyproject.toml).",
        Parameters: llm.ToolParameters{ Type: "object", Properties: map[string]llm.ToolProperty{} },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "module_graph",
        Description: "Show which of this Go module's own packages " +
          "import which — internal architecture, not the full " +
          "stdlib/third-party dependency closure.",
        Parameters: llm.ToolParameters{ Type: "object", Properties: map[string]llm.ToolProperty{} },
      },
    },

    // --- documentation ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "read_doc_comment",
        Description: "Get just a symbol's doc comment, without " +
          "reading the whole file it's defined in.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "name": {
              Type:        "string",
              Description: "Symbol name to look up",
            },
            "from_file": {
              Type:        "string",
              Description: "File you're currently looking at, to disambiguate a common name",
            },
          },
          Required: []string{ "name" },
        },
      },
    },

    // --- call graph (Go only, syntactic — not type-checked) ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "find_callers",
        Description: "Find syntactic call sites of a Go function/" +
          "method name — matched by identifier text, not type-checked, " +
          "so treat results as leads, not certainties.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "name": {
              Type:        "string",
              Description: "Function or method name to find call sites of",
            },
          },
          Required: []string{ "name" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "find_dead_code",
        Description: "Find exported Go functions/methods with no " +
          "syntactic call sites anywhere in the project — a " +
          "possibly-unused signal, not proof; can false-positive, " +
          "notably on exported struct fields (ctags can't " +
          "distinguish them from methods).",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "Limit the scan to this file/directory prefix",
            },
          },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "impact_analysis",
        Description: "Find functions that transitively call a given " +
          "Go function, up to a bounded depth — what might need " +
          "re-checking if it changes.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "name": {
              Type:        "string",
              Description: "Function or method name to analyze",
            },
            "depth": {
              Type:        "integer",
              Description: "How many call levels out to trace (default 2, max 3)",
            },
          },
          Required: []string{ "name" },
        },
      },
    },

    // --- semantic search ---
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "semantic_search",
        Description: "Keyword/term-frequency relevance search across " +
          "the codebase — NOT true embedding-based semantic search. " +
          "Useful when you don't know the exact text to grep for; " +
          "prefer search_code when you do.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "query": {
              Type:        "string",
              Description: "Natural-language description of what you're looking for",
            },
            "count": {
              Type:        "integer",
              Description: "Number of results to return (default 5, max 15)",
            },
          },
          Required: []string{ "query" },
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
// model streams its reply; onToolCall, if non-nil, is called just
// before each tool call executes. Both are per-call parameters
// rather than Loop fields — lspserver dispatches requests onto
// their own goroutines against one shared *Loop, so a mutable
// Loop-level callback would race across concurrent asks/hovers.
func (l *Loop) Turn(
  ctx context.Context, history *[]llm.Message,
  onToken llm.OnToken, onToolCall func(name string, args map[string]any),
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
      if onToolCall != nil {
        onToolCall(call.Function.Name, call.Function.Arguments)
      }

      result := l.execute(ctx, call)
      resultJSON, err := json.Marshal(result)
      if err != nil {
        resultJSON = []byte(fmt.Sprintf(
          `{"error":"failed to encode tool result: %s"}`, err,
        ))
      }

      *history = append(*history, llm.Message{
        Role:       "tool",
        ToolName:   call.Function.Name,
        ToolCallID: call.ID,
        Content:    string(resultJSON),
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

  case "web_search":
    var in tools.WebSearchInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.WebSearch(ctx, l.WebSearch, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "count_lines":
    var in tools.CountLinesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.CountLines(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "list_languages":
    var in tools.ListLanguagesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ListLanguages(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "find_files":
    var in tools.FindFilesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FindFiles(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "file_stat":
    var in tools.FileStatInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FileStat(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "find_todo_fixme":
    var in tools.FindTodoFixmeInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FindTodoFixme(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "env_info":
    var in tools.EnvInfoInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.EnvInfo(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "project_manifest":
    var in tools.ProjectManifestInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ProjectManifest(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "find_definition":
    var in tools.FindDefinitionInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FindDefinition(ctx, l.Symbols, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "list_symbols":
    var in tools.ListSymbolsInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ListSymbols(ctx, l.Symbols, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "outline_file":
    var in tools.OutlineFileInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.OutlineFile(ctx, l.Symbols, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "find_references":
    var in tools.FindReferencesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FindReferences(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "git_log":
    var in tools.GitLogInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.GitLog(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "git_blame":
    var in tools.GitBlameInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.GitBlame(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "git_diff":
    var in tools.GitDiffInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.GitDiff(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "git_show":
    var in tools.GitShowInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.GitShow(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "working_tree_diff":
    var in tools.WorkingTreeDiffInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.WorkingTreeDiff(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "grep_history":
    var in tools.GrepHistoryInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.GrepHistory(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "code_owners":
    var in tools.CodeOwnersInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.CodeOwners(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "commit_stats":
    var in tools.CommitStatsInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.CommitStats(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "run_tests":
    var in tools.RunTestsInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.RunTests(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "build_check":
    var in tools.BuildCheckInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.BuildCheck(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "lint_check":
    var in tools.LintCheckInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.LintCheck(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "list_dependencies":
    var in tools.ListDependenciesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ListDependencies(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "module_graph":
    var in tools.ModuleGraphInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ModuleGraph(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "read_doc_comment":
    var in tools.ReadDocCommentInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ReadDocComment(ctx, l.Symbols, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "find_callers":
    var in tools.FindCallersInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FindCallers(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "find_dead_code":
    var in tools.FindDeadCodeInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.FindDeadCode(ctx, l.Symbols, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "impact_analysis":
    var in tools.ImpactAnalysisInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.ImpactAnalysis(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }
    return out

  case "semantic_search":
    var in tools.SemanticSearchInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }
    out, err := tools.SemanticSearch(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
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
