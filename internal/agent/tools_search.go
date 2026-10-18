package agent

// Tools that search: the code, the web, the reference library.

import (
  "context"

  "armin/internal/library"
  "armin/internal/llm"
  "armin/internal/tools"
)

var searchTools = []toolSpec{
  {
    id:    toolSearchCode,
    run:   inSandbox(tools.SearchCode),
    paths: pathArg,
    def: llm.ToolFunction{
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
            Description: "Directory or file to search under (default: " +
              "project root)",
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
    id:    toolWebSearch,
    run:   runWebSearch,
    def: llm.ToolFunction{
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
            Description: "general: web pages and docs (default); code: " +
              "Stack Overflow and GitHub; science: papers (arXiv, Semantic " +
              "Scholar, OpenAlex)",
            Enum:        []string{ "general", "code", "science" },
          },
        },
        Required: []string{ "query" },
      },
    },
  },
  {
    id:    toolFindTodoFixme,
    run:   inSandbox(tools.FindTodoFixme),
    def: llm.ToolFunction{
      Name: "find_todo_fixme",
      Description: "Find TODO/FIXME/XXX comment markers in the " +
        "codebase.",
      Parameters: llm.ToolParameters{
        Type: "object",
        Properties: map[string]llm.ToolProperty{
          "path": {
            Type:        "string",
            Description: "Directory or file to search under (default: " +
              "project root)",
          },
        },
      },
    },
  },
  {
    id:    toolSemanticSearch,
    run:   inSandbox(tools.SemanticSearch),
    def: llm.ToolFunction{
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
            Description: "Natural-language description of what you're " +
              "looking for",
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
  {
    id:    toolLibrarySearch,
    flags: flagAgent | flagSession | flagLibrary,
    run:   runLibrarySearch,
    def: toolFunc(
      "library_search",
      "Search the user's reference library (books, official docs, "+
        "language standards, RFCs, man pages) for passages on a "+
        "technical question. Returns the best passages with title, "+
        "file, page or line, and how to read more. Cite what you use "+
        "as [Title, page N].",
      map[string]llm.ToolProperty{
        "query": stringProp(
          "Keywords, e.g. \"happens before channel close\" or " +
            "\"epoll edge triggered\"",
        ),
        "count": {
          Type:        "integer",
          Description: "Passages to return (default 5, max 10)",
        },
        "topic": stringProp(
          "Optional: keep to the books of one topic (see library_topics), " +
            "e.g. concurrency, sql, linear-algebra",
        ),
      },
      "query",
    ),
  },
  {
    id:    toolLibraryTopics,
    flags: flagAgent | flagSession | flagLibrary,
    run:   runLibraryTopics,
    def: toolFunc(
      "library_topics",
      "Show where the library covers a topic: book, chapters or pages, "+
        "and what it's good for. With no topic, lists every topic.",
      map[string]llm.ToolProperty{
        "topic": stringProp(
          "Topic name or part of it, e.g. memory, http, calculus",
        ),
      },
    ),
  },
}

func runWebSearch(ctx context.Context, l *Loop, c toolCall) any {
  in, bad := decode[tools.WebSearchInput](c)
  if bad != nil {
    return bad
  }

  out, err := tools.WebSearch(ctx, l.WebSearch, in)
  if err != nil {
    return errResult(err)
  }

  return out
}

func runLibrarySearch(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[libraryArgs](c)
  if bad != nil {
    return bad
  }

  res, err := l.Library.Search(in.Query, in.Count, in.Topic)
  if err != nil {
    return errResult(err)
  }

  if len(res) == 0 {
    return map[string]string{
      "results": "none",
      "hint": "nothing in the library matches; try other words, " +
        "drop the topic, or use web_search",
    }
  }

  return map[string][]library.Result{ "results": res }
}

func runLibraryTopics(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[libraryArgs](c)
  if bad != nil {
    return bad
  }

  rows := l.Library.TopicRows(in.Topic)
  if len(rows) == 0 {
    return map[string]string{
      "error": "no topic matches " + in.Topic + "; call " +
        "library_topics without a topic for the list, or " +
        "library_search directly",
    }
  }

  return map[string]any{ "topics": rows }
}
