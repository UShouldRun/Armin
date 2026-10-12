package tools

import (
  "context"
  "testing"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

// --- find_definition / list_symbols / outline_file: nil index ---

func TestFindDefinitionNilIndexIsCleanError(t *testing.T) {
  _, err := FindDefinition(context.Background(), nil, FindDefinitionInput{Name: "Foo"})
  if err == nil {
    t.Fatal("expected an error for a nil ctags index")
  }
}

func TestListSymbolsNilIndexIsCleanError(t *testing.T) {
  _, err := ListSymbols(context.Background(), nil, ListSymbolsInput{})
  if err == nil {
    t.Fatal("expected an error for a nil ctags index")
  }
}

func TestOutlineFileNilIndexIsCleanError(t *testing.T) {
  in := OutlineFileInput{Path: "a.go"}
  _, err := OutlineFile(context.Background(), nil, in)
  if err == nil {
    t.Fatal("expected an error for a nil ctags index")
  }
}

func TestFindDefinitionRequiresName(t *testing.T) {
  idx := ctags.New(t.TempDir(), "")
  _, err := FindDefinition(context.Background(), idx, FindDefinitionInput{})
  if err == nil {
    t.Fatal("expected an error for an empty name")
  }
}

// --- degrade-gracefully with a real index, no binary configured ---
// (a "" binary means the index's scan always fails silently and
// serves an empty result set — see internal/ctags — so this
// exercises the same path a missing universal-ctags binary hits in
// production, no real ctags install required for the test itself.)

func TestFindDefinitionEmptyIndexReturnsNoMatches(t *testing.T) {
  idx := ctags.New(t.TempDir(), "")
  out, err := FindDefinition(context.Background(), idx, FindDefinitionInput{Name: "Foo"})
  if err != nil {
    t.Fatalf("FindDefinition: %v", err)
  }
  if len(out.Matches) != 0 {
    t.Fatalf("Matches = %+v, want none", out.Matches)
  }
}

func TestOutlineFileRequiresPath(t *testing.T) {
  idx := ctags.New(t.TempDir(), "")
  _, err := OutlineFile(context.Background(), idx, OutlineFileInput{})
  if err == nil {
    t.Fatal("expected an error for an empty path")
  }
}

// --- find_references (ripgrep-backed, not ctags) ---

func TestFindReferencesFindsWordBoundaryMatches(t *testing.T) {
  requireRipgrep(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "func Needle() {}\n\nfunc main() { Needle() }\n")

  out, err := FindReferences(context.Background(), cfg, FindReferencesInput{Name: "Needle"})
  if err != nil {
    t.Fatalf("FindReferences: %v", err)
  }
  if out.Matches == "(no references found)" {
    t.Fatal("expected to find references to Needle")
  }
}

func TestFindReferencesRequiresName(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := FindReferences(context.Background(), cfg, FindReferencesInput{})
  if err == nil {
    t.Fatal("expected an error for an empty name")
  }
}

func TestFindReferencesNoMatches(t *testing.T) {
  requireRipgrep(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "package main\n")

  out, err := FindReferences(context.Background(), cfg, FindReferencesInput{Name: "DefinitelyNotPresent"})
  if err != nil {
    t.Fatalf("FindReferences: %v", err)
  }
  if out.Matches != "(no references found)" {
    t.Fatalf("Matches = %q, want the no-references sentinel", out.Matches)
  }
}
