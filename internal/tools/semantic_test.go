package tools

import (
  "context"
  "testing"

  "armin/internal/sandbox"
)

func TestTokenize(t *testing.T) {
  // Underscore isn't a letter or digit, so it splits like any other
  // punctuation — tokenize doesn't treat "_" as part of an
  // identifier the way a real code tokenizer would.
  got := tokenize("Hello, World! foo-bar_baz 123")
  want := []string{ "hello", "world", "foo", "bar", "baz", "123" }
  if len(got) != len(want) {
    t.Fatalf("tokenize = %v, want %v", got, want)
  }
  for i := range want {
    if got[i] != want[i] {
      t.Fatalf("tokenize[%d] = %q, want %q", i, got[i], want[i])
    }
  }
}

func TestSemanticSearchRanksByTermFrequency(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package a\n// sandbox sandbox sandbox path jail\n")
  writeFile(t, cfg.Root, "b.go", "package b\n// mentions sandbox once\n")
  writeFile(t, cfg.Root, "c.go", "package c\n// unrelated content entirely\n")

  out, err := SemanticSearch(context.Background(), cfg, SemanticSearchInput{ Query: "sandbox" })
  if err != nil {
    t.Fatalf("SemanticSearch: %v", err)
  }
  if len(out.Results) != 2 {
    t.Fatalf("Results = %+v, want 2 matches (c.go has no hits)", out.Results)
  }
  if out.Results[0].File != "a.go" {
    t.Fatalf("Results[0] = %+v, want a.go first (3 mentions > 1)", out.Results[0])
  }
}

func TestSemanticSearchRequiresQuery(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := SemanticSearch(context.Background(), cfg, SemanticSearchInput{})
  if err == nil {
    t.Fatal("expected an error for an empty query")
  }
}

func TestSemanticSearchNoMatches(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package a\n")

  out, err := SemanticSearch(context.Background(), cfg, SemanticSearchInput{ Query: "definitelynotpresent" })
  if err != nil {
    t.Fatalf("SemanticSearch: %v", err)
  }
  if len(out.Results) != 0 {
    t.Fatalf("Results = %+v, want none", out.Results)
  }
}
