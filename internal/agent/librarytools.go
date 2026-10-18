package agent

import (
  "encoding/json"

  "armin/internal/library"
  "armin/internal/llm"
)

// libraryDefinitions returns the library tools when one is
// configured.
func (l *Loop) libraryDefinitions() []llm.ToolDefinition {
  if l.Library == nil {
    return nil
  }

  return []llm.ToolDefinition{
    noteDef(
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
    noteDef(
      "library_topics",
      "Show where the library covers a topic: book, chapters or pages, "+
        "and what it's good for. With no topic, lists every topic.",
      map[string]llm.ToolProperty{
        "topic": stringProp(
          "Topic name or part of it, e.g. memory, http, calculus",
        ),
      },
    ),
  }
}

// executeLibrary runs a library tool; ok is false for any other
// tool.
func (l *Loop) executeLibrary(id ToolID, raw []byte) (any, bool) {
  if id != toolLibrarySearch && id != toolLibraryTopics {
    return nil, false
  }

  if l.Library == nil {
    return map[string]string{
      "error": "no library is configured (library = <dir> in config.conf)",
    }, true
  }

  var in struct {
    Query string `json:"query"`
    Count int    `json:"count"`
    Topic string `json:"topic"`
  }

  err := json.Unmarshal(raw, &in)
  if err != nil {
    return malformed("library", err), true
  }

  if id == toolLibraryTopics {
    rows := l.Library.TopicRows(in.Topic)
    if len(rows) == 0 {
      return map[string]string{
        "error": "no topic matches " + in.Topic + "; call " +
          "library_topics without a topic for the list, or " +
          "library_search directly",
      }, true
    }

    return map[string]any{ "topics": rows }, true
  }

  res, err := l.Library.Search(in.Query, in.Count, in.Topic)
  if err != nil {
    return errResult(err), true
  }

  if len(res) == 0 {
    return map[string]string{
      "results": "none",
      "hint": "nothing in the library matches; try other words, " +
        "drop the topic, or use web_search",
    }, true
  }

  return map[string][]library.Result{ "results": res }, true
}
