package agent

// Tools that read git history. All read-only.

import (
  "armin/internal/llm"
  "armin/internal/tools"
)

var gitTools = []toolSpec{
  {
    id:    toolGitLog,
    run:   inSandbox(tools.GitLog),
    def: llm.ToolFunction{
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
    id:    toolGitBlame,
    run:   inSandbox(tools.GitBlame),
    def: llm.ToolFunction{
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
    id:    toolGitDiff,
    run:   inSandbox(tools.GitDiff),
    def: llm.ToolFunction{
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
    id:    toolGitShow,
    run:   inSandbox(tools.GitShow),
    def: llm.ToolFunction{
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
    id:    toolWorkingTreeDiff,
    run:   inSandbox(tools.WorkingTreeDiff),
    def: llm.ToolFunction{
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
    id:    toolGrepHistory,
    run:   inSandbox(tools.GrepHistory),
    def: llm.ToolFunction{
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
    id:    toolCodeOwners,
    run:   inSandbox(tools.CodeOwners),
    def: llm.ToolFunction{
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
    id:    toolCommitStats,
    run:   inSandbox(tools.CommitStats),
    def: llm.ToolFunction{
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
}
