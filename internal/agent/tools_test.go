package agent

import (
  "context"
  "os"
  "path/filepath"
  "testing"

  "armin/internal/library"
  "armin/internal/llm"
)

func TestLibraryTools(t *testing.T) {
  if (&Loop{}).offered(toolLibrarySearch) {
    t.Fatal("library tools offered without a library")
  }

  root := t.TempDir()
  os.MkdirAll(filepath.Join(root, "go"), 0o755)
  os.WriteFile(filepath.Join(root, "go", "Effective Go.md"),
    []byte("# Concurrency\nShare memory by communicating.\n"), 0o644)
  lib, err := library.Open(root)
  if err != nil {
    t.Fatal(err)
  }

  err = lib.Build(context.Background(), nil)
  if err != nil {
    t.Fatal(err)
  }

  l := &Loop{ Library: lib }
  if !l.offered(toolLibrarySearch) || !l.offered(toolLibraryTopics) {
    t.Fatal("library tools missing with a library")
  }

  call := func(name string, args map[string]any) any {
    return l.execute(context.Background(), llm.ToolCall{
      Function: llm.ToolCallFunction{ Name: name, Arguments: args },
    }, Hooks{})
  }

  out := call("library_search", map[string]any{ "query": "share memory" })
  res, _ := out.(map[string][]library.Result)
  found := res["results"]
  if len(found) != 1 || found[0].Title != "Effective Go" {
    t.Fatalf("library_search = %#v", out)
  }

  out = call("library_topics", map[string]any{ "topic": "nothing" })
  m, _ := out.(map[string]string)
  if m["error"] == "" {
    t.Fatalf("library_topics on an empty table = %#v", out)
  }
}
