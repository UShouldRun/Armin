package agent

import (
  "context"
  "os"
  "path/filepath"
  "testing"

  "armin/internal/library"
)

func TestLibraryTools(t *testing.T) {
  l := &Loop{}
  if len(l.libraryDefinitions()) != 0 {
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

  l.Library = lib
  if len(l.libraryDefinitions()) != 2 {
    t.Fatal("library tools missing with a library")
  }

  query := []byte(`{"query":"share memory"}`)
  out, ok := l.executeLibrary(toolLibrarySearch, query)
  res, _ := out.(map[string][]library.Result)
  found := res["results"]
  if !ok || len(found) != 1 || found[0].Title != "Effective Go" {
    t.Fatalf("library_search = %#v", out)
  }

  out, ok = l.executeLibrary(toolLibraryTopics, []byte(`{"topic":"nothing"}`))
  m, _ := out.(map[string]string)
  if !ok || m["error"] == "" {
    t.Fatalf("library_topics on an empty table = %#v", out)
  }

  _, ok = l.executeLibrary(toolReadFile, []byte(`{}`))
  if ok {
    t.Fatal("executeLibrary claimed read_file")
  }
}
