// Package agent implements the tool-calling round trip: send
// messages to the model, execute any tool calls it asks for, feed
// results back, and repeat until it answers in plain text.
package agent

import (
  "bytes"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "sync"

  "armin/internal/convo"
  "armin/internal/ctags"
  "armin/internal/llm"
  "armin/internal/notes"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/tools"
)

// DefaultMaxRounds bounds how many tool-call round trips one user
// turn may take. Real edit-build-fix work needs dozens; the cap only
// stops a model that's looping.
const DefaultMaxRounds = 50

// Loop wires an LLM client to the sandboxed tool implementations.
type Loop struct {
  LLM     *llm.Client
  Sandbox *sandbox.Config

  // WebSearch backs the web_search tool. Nil disables it gracefully
  // — the tool is still advertised (see ToolDefinitions), but every
  // call returns a clean "unavailable" error instead of a panic.
  WebSearch *searxng.Manager

  // Symbols backs the ctags-based tools; nil degrades the same way
  // WebSearch does.
  Symbols *ctags.Index

  // Tracker remembers what the model has read, so write_file and
  // edit_file never act on a file it hasn't seen (or that changed
  // since).
  Tracker *tools.FileTracker

  // Memories, Skills and Scratch back the note tools; each one left
  // nil drops its tools from the definitions.
  Memories *notes.Memories
  Skills   *notes.Skills
  Scratch  *notes.Scratchpad

  // MaxRounds overrides DefaultMaxRounds.
  MaxRounds int

  // ContextSize is the model's context window in tokens; zero
  // disables automatic compaction. Ignored when Window is set.
  ContextSize int

  // Window, if set, is a model with a resident and a streaming
  // context tier; manageContext moves between them (see Decide).
  Window WindowControl

  // KeepTurns is how many recent user turns keep their full tool
  // results; older ones are reduced to a one-line stub (see
  // elideTurn). Zero uses DefaultKeepTurns, negative disables.
  KeepTurns int

  // ReadOnly drops write_file/edit_file (-lsp has nobody to approve
  // a write).
  ReadOnly bool

  defsOnce   sync.Once
  defs       []llm.ToolDefinition
  params     map[string]map[string]bool // tool → its parameter names
  defsTokens int // rough size of defs, sent with every request
}

// Hooks are the per-call callbacks of Turn. They're parameters, not
// Loop fields: lspserver runs concurrent turns against one Loop.
// Every field may be nil.
type Hooks struct {
  // Token and Reasoning receive streamed content and thinking.
  Token, Reasoning llm.OnToken

  // ToolCall fires just before each tool call executes.
  ToolCall func(name string, args map[string]any)

  // ToolResult fires after it, with the result as sent to the model
  // (JSON); ResultLine summarizes it for display.
  ToolResult func(name, result string)

  // Approve is asked before a file write, with a diff preview. A
  // false return declines it; feedback, if any, goes to the model.
  // Nil approves everything.
  Approve func(tool, preview string) (ok bool, feedback string)

  // Compacted fires after Turn rewrote history in place (compaction
  // or elision), so the caller can resync anything mirroring it.
  Compacted func(before, after int)

  // Usage receives the server's token count after each model call.
  Usage func(llm.Usage)

  // Notice receives one-line status messages about context
  // management (growing the window, saving notes, compacting).
  Notice func(string)
}

// New returns a ready-to-use Loop.
func New(client *llm.Client, sb *sandbox.Config) *Loop {
  return &Loop{
    LLM:     client,
    Sandbox: sb,
    Tracker: tools.NewFileTracker(),
  }
}

// Definitions is what this Loop actually offers the model:
// ToolDefinitions minus write tools when ReadOnly, plus whichever
// note tools are configured. Built once.
func (l *Loop) Definitions() []llm.ToolDefinition {
  l.defsOnce.Do(func() {
    for _, d := range ToolDefinitions() {
      if l.ReadOnly && toolIDs[d.Function.Name].isWrite() {
        continue
      }

      l.defs = append(l.defs, d)
    }

    l.defs = append(l.defs, l.noteDefinitions()...)
    raw, _ := json.Marshal(l.defs)
    l.defsTokens = len(raw) / 4

    l.params = make(map[string]map[string]bool, len(l.defs))
    for _, d := range l.defs {
      names := make(map[string]bool, len(d.Function.Parameters.Properties))
      for k := range d.Function.Parameters.Properties {
        names[k] = true
      }

      l.params[d.Function.Name] = names
    }
  })

  return l.defs
}

// Estimate is the rough size of the next request: history plus the
// tool definitions that go with every request.
func (l *Loop) Estimate(history []llm.Message) int {
  l.Definitions()
  return convo.Analyze(history).ApproxTokens + l.defsTokens
}

func (l *Loop) maxRounds() int {
  if l.MaxRounds > 0 {
    return l.MaxRounds
  }

  return DefaultMaxRounds
}

// ToolDefinitions returns the schema for the 37 sandboxed tools —
// everything except the note tools, which need a Loop's stores (see
// Definitions). Descriptions are kept short and concrete — small
// models are sensitive to prompt bloat.
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
        Name: "read_pdf",
        Description: "Extract the text of a PDF (slides, papers, " +
          "manuals), a page range at a time, with --- page N --- " +
          "markers. Default: 10 pages from first_page.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "path": {
              Type:        "string",
              Description: "PDF path, relative to the project or absolute in a directory the user added",
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
              Items:       &llm.ToolProperty{ Type: "string" },
            },
          },
          Required: []string{ "binary" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
        Name: "run_program",
        Description: "Build, compile, test or run code: make, cmake, gcc, " +
          "g++, clang, mpicc, mpirun, go, cargo, python3, node, npm... " +
          "or a program the project built (\"./build/app\"). No shell: " +
          "binary plus an args array. The user approves each run. " +
          "Prefer run_tests / build_check when they fit.",
        Parameters: llm.ToolParameters{
          Type: "object",
          Properties: map[string]llm.ToolProperty{
            "binary": {
              Type:        "string",
              Description: "Program: a bare name (make, gcc, mpirun) or a relative path inside the project (./fox)",
            },
            "args": {
              Type:        "array",
              Description: `Arguments as separate entries, e.g. ["-O2", "-o", "fox", "src/fox.c"]`,
              Items:       &llm.ToolProperty{ Type: "string" },
            },
            "dir": {
              Type:        "string",
              Description: "Working directory inside the project (default: project root), e.g. \"build\"",
            },
          },
          Required: []string{ "binary" },
        },
      },
    },
    {
      Type: "function",
      Function: llm.ToolFunction{
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
      Type: "function",
      Function: llm.ToolFunction{
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
            "topic": {
              Type:        "string",
              Description: "general: web pages and docs (default); code: Stack Overflow and GitHub; science: papers (arXiv, Semantic Scholar, OpenAlex)",
              Enum:        []string{ "general", "code", "science" },
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
    projectRunDef(
      "run_tests",
      "Run the project's test command (detected from Makefile, "+
        "go.mod, Cargo.toml, package.json, or Python config; or "+
        "user-configured). Reports pass/fail, a failure summary, "+
        "and the end of the output.",
      `Extra arguments replacing the default target, e.g. `+
        `["./internal/tools", "-run", "TestX"] or ["-k", "parser"]`,
    ),
    projectRunDef(
      "build_check",
      "Run the project's build or type-check command and report "+
        "pass/fail plus errors. Run it after editing code.",
      `Extra arguments replacing the default target, e.g. `+
        `["./cmd/..."]`,
    ),
    projectRunDef(
      "lint_check",
      "Run the project's linter; reports available:false if the "+
        "project has none.",
      "Extra arguments replacing the default target",
    ),

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

// roundLimitNote is sent when a turn runs out of tool rounds: one
// last call, with no tools, so the user gets a report instead of an
// error.
const roundLimitNote = "You have reached the tool-call limit for " +
  "this turn. Don't call any more tools. Report what you did, what " +
  "is done and verified, and what is left, so the user can decide " +
  "how to continue."

// maxBlocked is how many repeats the guard refuses before the turn
// stops: a model that keeps retrying a blocked call won't stop on
// its own.
const maxBlocked = 2

const repeatNote = "[armin] You kept repeating a call that had " +
  "already failed or that the user declined, so tools are off for " +
  "the rest of this turn. Don't call any more tools. Tell the user " +
  "what you were trying to do, what failed, and — if they gave " +
  "feedback — how you'll follow it, then ask how to continue."

// Turn runs one user turn to completion: it sends history, executes
// any tool calls the model makes, feeds results back, and returns
// once the model answers in plain text. history is mutated in place
// so callers can persist it across turns.
func (l *Loop) Turn(
  ctx context.Context, history *[]llm.Message, hooks Hooks,
) (string, error) {
  defs := l.Definitions()
  stream := llm.StreamHandlers{
    Token: hooks.Token, Reasoning: hooks.Reasoning,
  }

  turnStart := len(*history)
  var last llm.Usage
  sources := &turnSources{}
  failed := map[string]string{} // name+args → error since the last success
  declined := map[string]bool{}  // directories the user refused this turn
  blocked := 0                   // repeats the guard refused this turn
  ran := map[string]string{}     // run tool call → its result, this turn

  defer func() { l.endTurn(history, turnStart, hooks) }()

  stopNote := roundLimitNote

  for round := 0; round < l.maxRounds(); round++ {
    if l.manageContext(ctx, history, last, hooks) {
      turnStart = 0 // indices moved; the end-of-turn pass rescans
      last = llm.Usage{}
    }

    reply, err := l.LLM.ChatStream(ctx, *history, defs, stream)
    if err != nil {
      return "", fmt.Errorf(
        "\n\t... in Turn:\n\t model call failed: %w", err,
      )
    }

    last = reply.Usage
    if hooks.Usage != nil && last.Total() > 0 {
      hooks.Usage(last)
    }

    *history = append(*history, *reply)

    if len(reply.ToolCalls) == 0 {
      return l.finish(history, sources, hooks), nil
    }

    for _, call := range reply.ToolCalls {
      if err := ctx.Err(); err != nil {
        return "", fmt.Errorf("\n\t... in Turn:\n\t%w", err)
      }

      normalizeArgs(&call)
      if hooks.ToolCall != nil {
        hooks.ToolCall(call.Function.Name, call.Function.Arguments)
      }

      key := callKey(call)
      var resultJSON []byte
      if prev, ok := failed[key]; ok {
        blocked++
        resultJSON, _ = json.Marshal(map[string]string{
          "error": "you already made this exact call this turn and it " +
            "failed: " + prev + " Don't repeat it: change the " +
            "arguments, use another tool, or stop and tell the user " +
            "what is blocking you.",
        })
      } else if blocked >= maxBlocked {
        // Once the turn is being wound down, the rest of this reply's
        // calls get a result (every call needs one) but don't run.
        resultJSON, _ = json.Marshal(map[string]string{
          "error": "not run: this turn is stopping",
        })
      } else {
        result := l.executeTurn(ctx, call, hooks, sources, declined)
        resultJSON, err = json.Marshal(result)
        if err != nil {
          resultJSON = []byte(fmt.Sprintf(
            `{"error":"failed to encode tool result: %s"}`, err,
          ))
        }

        // A success in between may have changed what a failed call
        // depends on (read_file before an edit), so only a repeat
        // with nothing working in between is blocked.
        if msg := errorOf(resultJSON); msg != "" {
          failed[key] = msg
        } else if prev, ok := ran[key]; ok && prev == string(resultJSON) && runTool(call.Function.Name) {
          // The same run, the same output: running it again won't
          // change anything.
          blocked++
          resultJSON, _ = json.Marshal(map[string]any{
            "error": "this is the same call you already ran this turn, " +
              "and it gave exactly the same result as before (below). " +
              "Running it again won't change anything: do something " +
              "else, or stop and tell the user.",
            "result": json.RawMessage(resultJSON),
          })
        } else {
          clear(failed)
        }

        if runTool(call.Function.Name) && errorOf(resultJSON) == "" {
          ran[key] = string(resultJSON)
        }
      }

      if hooks.ToolResult != nil {
        hooks.ToolResult(call.Function.Name, string(resultJSON))
      }

      *history = append(*history, llm.Message{
        Role:       "tool",
        ToolName:   call.Function.Name,
        ToolCallID: call.ID,
        Content:    string(resultJSON),
      })
    }

    if blocked >= maxBlocked {
      stopNote = repeatNote
      break
    }
  }

  *history = append(*history, llm.Message{
    Role: "user", Content: stopNote,
  })
  reply, err := l.LLM.ChatStream(ctx, *history, nil, stream)
  if err != nil {
    return "", fmt.Errorf(
      "\n\t... in Turn:\n\t stopping the turn (tool-round limit %d or "+
        "repeated calls), and the closing summary call failed: %w",
      l.maxRounds(), err,
    )
  }

  *history = append(*history, *reply)

  return l.finish(history, sources, hooks), nil
}

// unknownParams names arguments the tool doesn't have. Decoding
// would silently drop them — "arguments" for "args" ran a bare
// find — so they're an error the model can correct.
func (l *Loop) unknownParams(name string, args map[string]any) string {
  l.Definitions()
  known, ok := l.params[name]
  if !ok {
    return "" // unknown tools are reported by the dispatch
  }

  var bad []string
  for k := range args {
    if !known[k] {
      bad = append(bad, k)
    }
  }

  if len(bad) == 0 {
    return ""
  }

  valid := make([]string, 0, len(known))
  for k := range known {
    valid = append(valid, k)
  }

  sort.Strings(bad)
  sort.Strings(valid)

  return fmt.Sprintf(
    "%s has no parameter %s; its parameters are: %s. Nothing was run.",
    name, strings.Join(bad, ", "), strings.Join(valid, ", "),
  )
}

// argAliases are names small models use for run_command's and
// run_program's "args".
var argAliases = []string{ "arguments", "argv", "arg", "cmd_args", "params", "flags" }

// normalizeArgs fixes the shapes small models give run_command and
// run_program in place, before the call is shown or run: an alias
// for "args", args as one string, or the whole command line in
// "binary". The model's history then holds the fixed call, so it
// sees the right form.
func normalizeArgs(call *llm.ToolCall) {
  id := toolIDs[call.Function.Name]
  if id != toolRunCommand && id != toolRunProgram {
    return
  }

  a := call.Function.Arguments
  if a == nil {
    return
  }

  if _, ok := a["args"]; !ok {
    for _, alias := range argAliases {
      if v, ok := a[alias]; ok {
        a["args"] = v
        delete(a, alias)
        break
      }
    }
  }

  if s, ok := a["args"].(string); ok {
    a["args"] = toAny(splitWords(s))
  }

  bin, _ := a["binary"].(string)
  if words := splitWords(bin); len(words) > 1 {
    list, _ := a["args"].([]any)
    a["binary"] = words[0]
    a["args"] = append(toAny(words[1:]), list...)
  }
}

func toAny(words []string) []any {
  out := make([]any, len(words))
  for i, w := range words {
    out[i] = w
  }

  return out
}

// splitWords splits a command line the way a shell would split
// plain words and quotes — no variables, globs or escapes beyond
// quotes.
func splitWords(s string) []string {
  var (
    words []string
    cur   strings.Builder
    quote rune
    in    bool
  )

  for _, r := range s {
    switch {
    case quote != 0:
      if r == quote {
        quote = 0
      } else {
        cur.WriteRune(r)
      }

    case r == '\'' || r == '"':
      quote, in = r, true

    case r == ' ' || r == '\t' || r == '\n':
      if in {
        words = append(words, cur.String())
        cur.Reset()
        in = false
      }

    default:
      cur.WriteRune(r)
      in = true
    }
  }

  if in {
    words = append(words, cur.String())
  }

  return words
}

// runTools execute something; the same call returning the same
// result again is a loop, not progress.
func runTool(name string) bool {
  switch toolIDs[name] {
  case toolRunCommand, toolRunProgram, toolRunTests, toolBuildCheck, toolLintCheck:
    return true
  }

  return false
}

// callKey identifies a call by tool and arguments; json.Marshal
// sorts map keys, so equal arguments give equal keys.
func callKey(call llm.ToolCall) string {
  args, _ := json.Marshal(call.Function.Arguments)
  return call.Function.Name + string(args)
}

// errorOf returns a result's top-level "error", or "". The cheap
// substring test first: inside a JSON string every quote is escaped,
// so `"error":"` only appears as a real key.
func errorOf(result []byte) string {
  if !bytes.Contains(result, []byte(`"error":"`)) {
    return ""
  }

  var r struct {
    Error string `json:"error"`
  }

  json.Unmarshal(result, &r)

  return lastErrorLine(r.Error)
}

// finish appends a Sources list to the final answer when the turn
// searched the web and the model didn't list its sources itself,
// streaming the addition like the rest of the answer.
func (l *Loop) finish(
  history *[]llm.Message, sources *turnSources, hooks Hooks,
) string {
  h := *history
  final := &h[len(h) - 1]
  if add := sources.appendix(final.Content); add != "" {
    final.Content += add
    if hooks.Token != nil {
      hooks.Token(add)
    }
  }

  return final.Content
}

// executeTurn is execute plus the turn's citation numbering and the
// access check for reads outside the project.
func (l *Loop) executeTurn(
  ctx context.Context, call llm.ToolCall, hooks Hooks,
  sources *turnSources, declined map[string]bool,
) any {
  if out := l.askAccess(call, hooks, declined); out != nil {
    return out
  }

  out := l.execute(ctx, call, hooks)
  if ws, ok := out.(*tools.WebSearchOutput); ok {
    sources.number(ws)
  }

  return out
}

// askAccess handles a read pointed outside the project. Small models
// rarely think to call request_access first, so armin asks the user
// itself, before the call runs, and grants the directory on a yes.
// A directory declined once isn't asked about again this turn.
func (l *Loop) askAccess(
  call llm.ToolCall, hooks Hooks, declined map[string]bool,
) any {
  if hooks.Approve == nil {
    return nil // nobody to ask: the call fails with a pointer to -add-dir
  }

  for _, p := range readPaths(call) {
    var outside *sandbox.OutsideError
    if _, err := l.Sandbox.ResolvePath(p); !errors.As(err, &outside) {
      continue
    }

    dir := outside.Dir()
    if declined[dir] {
      return map[string]string{
        "error": "the user declined read access to " + dir + " this turn; don't ask again",
      }
    }

    ok, feedback := hooks.Approve(
      "request_access",
      "read access to " + dir + "\nneeded for: " +
        CallLine(call.Function.Name, call.Function.Arguments),
    )
    if !ok {
      declined[dir] = true
      return declinedResult("read access to " + dir, feedback)
    }

    if _, err := l.Sandbox.AddReadRoot(dir); err != nil {
      return map[string]string{ "error": err.Error() }
    }
  }

  return nil
}

// readPaths are the paths a read-only call will open.
func readPaths(call llm.ToolCall) []string {
  args := call.Function.Arguments
  switch toolIDs[call.Function.Name] {
  case toolReadFile, toolReadPDF, toolSearchCode, toolListDirectory:
    if p, _ := args["path"].(string); p != "" {
      return []string{ p }
    }

  case toolRunCommand:
    bin, _ := args["binary"].(string)
    list, _ := args["args"].([]any)
    strs := make([]string, 0, len(list))
    for _, a := range list {
      strs = append(strs, fmt.Sprint(a))
    }

    return tools.PathArgs(bin, strs)
  }

  return nil
}

// execute dispatches one tool call to its implementation and
// returns a JSON-marshalable result. Errors are returned as
// structured data in the result itself (see plan section 6), not as
// Go errors, so a bad call becomes something the model can read and
// self-correct from on the next round rather than a crash.
func (l *Loop) execute(
  ctx context.Context, call llm.ToolCall, hooks Hooks,
) any {
  name := call.Function.Name

  raw, err := json.Marshal(call.Function.Arguments)
  if err != nil {
    return map[string]string{
      "error": fmt.Sprintf("could not encode arguments: %s", err),
    }
  }

  id := toolIDs[name]
  if l.ReadOnly && id.isWrite() {
    return map[string]string{
      "error": "this session is read-only; " + name + " is unavailable",
    }
  }

  if msg := l.unknownParams(name, call.Function.Arguments); msg != "" {
    return map[string]string{ "error": msg }
  }

  if out, ok := l.executeNote(id, raw); ok {
    return out
  }

  if id == toolRequestAccess {
    return l.requestAccess(raw, hooks)
  }

  switch id {
  case toolWriteFile:
    var in tools.WriteFileInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    change, err := tools.PlanWrite(l.Sandbox, l.Tracker, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return l.applyChange(name, change, hooks)

  case toolEditFile:
    var in tools.EditFileInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    change, err := tools.PlanEdit(l.Sandbox, l.Tracker, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return l.applyChange(name, change, hooks)

  case toolSearchCode:
    var in tools.SearchCodeInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.SearchCode(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolReadFile:
    var in tools.ReadFileInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ReadFile(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    if resolved, err := l.Sandbox.ResolvePath(in.Path); err == nil {
      l.Tracker.Mark(resolved)
    }

    return out

  case toolListDirectory:
    var in tools.ListDirectoryInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ListDirectory(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolRunCommand:
    var in tools.RunCommandInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, cmdErr := tools.RunCommand(ctx, l.Sandbox, in)
    if cmdErr != nil {
      return cmdErr
    }

    return out

  case toolWebSearch:
    var in tools.WebSearchInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.WebSearch(ctx, l.WebSearch, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolCountLines:
    var in tools.CountLinesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.CountLines(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolListLanguages:
    var in tools.ListLanguagesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ListLanguages(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFindFiles:
    var in tools.FindFilesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FindFiles(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFileStat:
    var in tools.FileStatInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FileStat(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFindTodoFixme:
    var in tools.FindTodoFixmeInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FindTodoFixme(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolEnvInfo:
    var in tools.EnvInfoInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.EnvInfo(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolProjectManifest:
    var in tools.ProjectManifestInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ProjectManifest(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFindDefinition:
    var in tools.FindDefinitionInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FindDefinition(ctx, l.Symbols, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolListSymbols:
    var in tools.ListSymbolsInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ListSymbols(ctx, l.Symbols, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolOutlineFile:
    var in tools.OutlineFileInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.OutlineFile(ctx, l.Symbols, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFindReferences:
    var in tools.FindReferencesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FindReferences(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolGitLog:
    var in tools.GitLogInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.GitLog(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolGitBlame:
    var in tools.GitBlameInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.GitBlame(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolGitDiff:
    var in tools.GitDiffInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.GitDiff(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolGitShow:
    var in tools.GitShowInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.GitShow(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolWorkingTreeDiff:
    var in tools.WorkingTreeDiffInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.WorkingTreeDiff(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolGrepHistory:
    var in tools.GrepHistoryInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.GrepHistory(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolCodeOwners:
    var in tools.CodeOwnersInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.CodeOwners(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolCommitStats:
    var in tools.CommitStatsInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.CommitStats(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolRunTests:
    var in tools.RunTestsInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.RunTests(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolBuildCheck:
    var in tools.BuildCheckInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.BuildCheck(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolLintCheck:
    var in tools.LintCheckInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.LintCheck(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolRunProgram:
    var in tools.RunProgramInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    return l.runProgram(ctx, in, hooks)

  case toolListDependencies:
    var in tools.ListDependenciesInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ListDependencies(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolModuleGraph:
    var in tools.ModuleGraphInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ModuleGraph(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolReadDocComment:
    var in tools.ReadDocCommentInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ReadDocComment(ctx, l.Symbols, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFindCallers:
    var in tools.FindCallersInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FindCallers(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolFindDeadCode:
    var in tools.FindDeadCodeInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.FindDeadCode(ctx, l.Symbols, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolImpactAnalysis:
    var in tools.ImpactAnalysisInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ImpactAnalysis(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolReadPDF:
    var in tools.ReadPDFInput
    if err := json.Unmarshal(raw, &in); err != nil {
      return malformed(name, err)
    }

    out, err := tools.ReadPDF(ctx, l.Sandbox, in)
    if err != nil {
      return map[string]string{ "error": err.Error() }
    }

    return out

  case toolSemanticSearch:
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

// requestAccess asks the user to let the model read a directory
// outside the project. With no one to ask (no Approve hook) it says
// how the user can grant it instead.
func (l *Loop) requestAccess(raw []byte, hooks Hooks) any {
  var in struct {
    Dir, Reason string
  }

  if err := json.Unmarshal(raw, &in); err != nil {
    return malformed("request_access", err)
  }

  dir := strings.TrimSpace(in.Dir)
  switch {
  case dir == "":
    return map[string]string{ "error": "dir is required" }
  case strings.HasPrefix(dir, "~/"):
    if home, err := os.UserHomeDir(); err == nil {
      dir = filepath.Join(home, dir[2:])
    }
  case !filepath.IsAbs(dir):
    dir = filepath.Join(l.Sandbox.Root, dir)
  }

  dir = filepath.Clean(dir)
  if info, err := os.Stat(dir); err != nil || !info.IsDir() {
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
    "note":     "read tools accept absolute paths under " + resolved + " now (e.g. " + filepath.Join(resolved, "<file>") + ")",
  }
}

// ApprovePrivileged is the tool name Approve gets for a privileged
// run_program (rm, cp, mv, scp, rsync).
const ApprovePrivileged = "run_program (privileged)"

// AlwaysAsk reports whether an approval must reach the user even
// under -yes or "all this session": reads outside the project and
// privileged programs.
func AlwaysAsk(tool string) bool {
  return tool == "request_access" || tool == ApprovePrivileged
}

// runProgram checks a run_program call, asks the user (with nobody
// to ask, it doesn't run), then runs it.
func (l *Loop) runProgram(
  ctx context.Context, in tools.RunProgramInput, hooks Hooks,
) any {
  dir, _, err := tools.PlanProgram(l.Sandbox, in)
  if err != nil {
    return map[string]string{ "error": err.Error() }
  }

  if hooks.Approve == nil {
    return map[string]string{
      "error": "run_program needs the user's approval and nobody can " +
        "approve here; use run_tests / build_check, or ask the user to run it",
    }
  }

  preview := "$ " + strings.TrimSpace(sandbox.DescribeArgs(in.Binary, in.Args))
  if dir != l.Sandbox.Root {
    preview += "\nin " + dir
  }

  tool := "run_program"
  if sandbox.Privileged(in.Binary) {
    tool = ApprovePrivileged
    preview += "\n(privileged: deletes, moves, copies or sends files — always asked)"
  }

  ok, feedback := hooks.Approve(tool, preview)
  if !ok {
    return declinedResult("running this", feedback)
  }

  out, err := tools.RunProgram(ctx, l.Sandbox, in)
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
  if feedback = strings.TrimSpace(feedback); feedback != "" {
    msg += " The user said: \"" + feedback + "\". Do what they said " +
      "instead of retrying the same call; if it isn't clear, stop and ask them."
  } else {
    msg += " Don't retry it: change approach, or stop and ask the user."
  }

  return map[string]string{ "error": msg }
}

// applyChange asks hooks.Approve (if set) before writing.
func (l *Loop) applyChange(
  name string, change *tools.Change, hooks Hooks,
) any {
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

// projectRunDef is the shared schema of run_tests/build_check/
// lint_check.
func projectRunDef(name, description, argsHelp string) llm.ToolDefinition {
  return llm.ToolDefinition{
    Type: "function",
    Function: llm.ToolFunction{
      Name:        name,
      Description: description,
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "args": {
            Type:        "array",
            Description: argsHelp,
            Items:       &llm.ToolProperty{ Type: "string" },
          },
        },
      },
    },
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
