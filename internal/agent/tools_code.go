package agent

// Tools that understand the project: its symbols, structure and
// dependencies.

import (
  "armin/internal/llm"
  "armin/internal/tools"
)

var codeTools = []toolSpec{
  {
    id:    toolFindDefinition,
    run:   withSymbols(tools.FindDefinition),
    def: llm.ToolFunction{
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
            Description: "File you're currently looking at, to rank " +
              "same-file/same-dir matches first",
          },
        },
        Required: []string{ "name" },
      },
    },
  },
  {
    id:    toolListSymbols,
    run:   withSymbols(tools.ListSymbols),
    def: llm.ToolFunction{
      Name: "list_symbols",
      Description: "Fuzzy-search every known symbol name in the " +
        "project (functions, types, variables, ...).",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "query": {
            Type:        "string",
            Description: "Substring to match against symbol names (empty " +
              "lists everything, capped)",
          },
        },
      },
    },
  },
  {
    id:    toolOutlineFile,
    run:   withSymbols(tools.OutlineFile),
    def: llm.ToolFunction{
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
    id:    toolFindReferences,
    run:   inSandbox(tools.FindReferences),
    def: llm.ToolFunction{
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
            Description: "Directory or file to search under (default: " +
              "project root)",
          },
        },
        Required: []string{ "name" },
      },
    },
  },
  {
    id:    toolReadDocComment,
    run:   withSymbolsAndSandbox(tools.ReadDocComment),
    def: llm.ToolFunction{
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
            Description: "File you're currently looking at, to " +
              "disambiguate a common name",
          },
        },
        Required: []string{ "name" },
      },
    },
  },
  {
    id:    toolFindCallers,
    run:   inSandbox(tools.FindCallers),
    def: llm.ToolFunction{
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
    id:    toolFindDeadCode,
    run:   withSymbolsAndSandbox(tools.FindDeadCode),
    def: llm.ToolFunction{
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
    id:    toolImpactAnalysis,
    run:   inSandbox(tools.ImpactAnalysis),
    def: llm.ToolFunction{
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
  {
    id:    toolListDependencies,
    run:   inSandbox(tools.ListDependencies),
    def: llm.ToolFunction{
      Name: "list_dependencies",
      Description: "List the project's declared dependencies, " +
        "parsed from whichever manifest exists (go.mod, " +
        "package.json, requirements.txt, Cargo.toml, pyproject.toml).",
      Parameters: llm.ToolParameters{
        Type:       "object",
        Properties: map[string]llm.ToolProperty{},
      },
    },
  },
  {
    id:    toolModuleGraph,
    run:   inSandbox(tools.ModuleGraph),
    def: llm.ToolFunction{
      Name: "module_graph",
      Description: "Show which of this Go module's own packages " +
        "import which — internal architecture, not the full " +
        "stdlib/third-party dependency closure.",
      Parameters: llm.ToolParameters{
        Type:       "object",
        Properties: map[string]llm.ToolProperty{},
      },
    },
  },
  {
    id:    toolListLanguages,
    run:   inSandbox(tools.ListLanguages),
    def: llm.ToolFunction{
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
    id:    toolEnvInfo,
    run:   inSandbox(tools.EnvInfo),
    def: llm.ToolFunction{
      Name: "env_info",
      Description: "Report the toolchain available: Go version, " +
        "OS/arch, and whether git/ripgrep/docker are installed.",
      Parameters: llm.ToolParameters{
        Type:       "object",
        Properties: map[string]llm.ToolProperty{},
      },
    },
  },
  {
    id:    toolProjectManifest,
    run:   inSandbox(tools.ProjectManifest),
    def: llm.ToolFunction{
      Name: "project_manifest",
      Description: "Get an orientation snapshot of the project: " +
        "directory tree plus the first README/manifest file found. " +
        "Useful for \"what kind of project is this\" mid-conversation.",
      Parameters: llm.ToolParameters{
        Type:       "object",
        Properties: map[string]llm.ToolProperty{},
      },
    },
  },
}
