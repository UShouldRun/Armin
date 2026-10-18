// Symbols from the ctags index: FindDefinition, ListSymbols,
// OutlineFile, and FindReferences (a word-boundary search).

package tools

import (
  "context"
  "fmt"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

func toSymbolLocation(s ctags.Symbol) SymbolLocation {
  return SymbolLocation{
    Name: s.Name, Kind: ctags.KindLabel(s.Kind), File: s.File,
    Line: s.Line, Scope: s.Scope,
  }
}

func toSymbolLocations(syms []ctags.Symbol) []SymbolLocation {
  out := make([]SymbolLocation, len(syms))
  for i, s := range syms {
    out[i] = toSymbolLocation(s)
  }
  return out
}

// ctagsUnavailable is the shared error every ctags-backed tool
// returns for a nil *ctags.Index — no universal-ctags binary was
// found at startup (see -ctags-bin), same degrade-gracefully pattern
// web_search uses for a nil *searxng.Manager.
func ctagsUnavailable(fn string) error {
  return fmt.Errorf(
    "\n\t... in %s:\n\t symbol lookup unavailable — no "+
      "universal-ctags binary was found at startup", fn,
  )
}

// --- find_definition ---

// FindDefinition wraps the same ctags index the LSP server's real
// textDocument/definition uses — a deterministic fact, not a model
// guess, now exposed to the agent directly instead of only over LSP.
func FindDefinition(
  ctx context.Context, idx *ctags.Index, in FindDefinitionInput,
) (*FindDefinitionOutput, error) {
  if idx == nil {
    return nil, ctagsUnavailable("FindDefinition")
  }
  if in.Name == "" {
    return nil, fmt.Errorf("\n\t... in FindDefinition:\n\t name is required")
  }

  matches := idx.Lookup(ctx, in.Name, in.FromFile)
  return &FindDefinitionOutput{ Matches: toSymbolLocations(matches) }, nil
}

// --- list_symbols ---

const maxListSymbolsResults = 200

// ListSymbols wraps the ctags index's fuzzy-ish name search —
// workspace/symbol's underlying lookup, now callable mid-turn.
func ListSymbols(
  ctx context.Context, idx *ctags.Index, in ListSymbolsInput,
) (*ListSymbolsOutput, error) {
  if idx == nil {
    return nil, ctagsUnavailable("ListSymbols")
  }

  matches := idx.Search(ctx, in.Query)
  out := &ListSymbolsOutput{}
  if len(matches) > maxListSymbolsResults {
    matches = matches[:maxListSymbolsResults]
    out.Truncated = true
  }
  out.Matches = toSymbolLocations(matches)
  return out, nil
}

// --- outline_file ---

// OutlineFile lists Path's known symbols via ctags without reading
// its content — a cheaper first look at a big file than read_file's
// 400-line threshold, which otherwise forces a blind start_line/
// end_line guess.
func OutlineFile(
  ctx context.Context, idx *ctags.Index, in OutlineFileInput,
) (*OutlineFileOutput, error) {
  if idx == nil {
    return nil, ctagsUnavailable("OutlineFile")
  }
  if in.Path == "" {
    return nil, fmt.Errorf("\n\t... in OutlineFile:\n\t path is required")
  }

  all := idx.Search(ctx, "") // every known symbol, filtered below by file
  var out OutlineFileOutput
  for _, m := range all {
    if m.File == in.Path {
      out.Symbols = append(out.Symbols, toSymbolLocation(m))
    }
  }
  return &out, nil
}

// --- find_references ---

// FindReferences is search_code's word-boundary-matched sibling: it
// finds where an identifier is USED, not where ctags says it's
// DEFINED (find_definition) — ctags only indexes definition sites,
// not call sites.
func FindReferences(
  ctx context.Context, cfg *sandbox.Config, in FindReferencesInput,
) (*FindReferencesOutput, error) {
  if in.Name == "" {
    return nil, fmt.Errorf("\n\t... in FindReferences:\n\t name is required")
  }

  searchPath, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FindReferences:\n\t%w", err)
  }

  args := []string{
    "--line-number", "--context", "1", "--max-columns", "300",
    "--word-regexp", in.Name, searchPath,
  }
  res, err := cfg.Exec(ctx, sandbox.Command{ Binary: "rg", Args: args, Mode: sandbox.ExecForce })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FindReferences:\n\t%w", err)
  }
  if res.ExitCode == 1 && res.Stdout == "" {
    return &FindReferencesOutput{ Matches: "(no references found)" }, nil
  }
  if res.ExitCode > 1 {
    return nil, fmt.Errorf(
      "\n\t... in FindReferences:\n\t rg failed: %s", res.Stderr,
    )
  }

  return &FindReferencesOutput{
    Matches: res.Stdout, Truncated: res.Flags.Truncated(),
  }, nil
}
