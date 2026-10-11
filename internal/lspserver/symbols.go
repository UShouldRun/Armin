package lspserver

import (
  "context"
  "encoding/json"
  "fmt"
  "path/filepath"
  "unicode"

  "armin/internal/ctags"
)

// maxWorkspaceSymbolResults caps workspace/symbol so a broad or
// empty query on a large project doesn't ship an enormous response.
const maxWorkspaceSymbolResults = 200

// definition answers textDocument/definition entirely from the
// ctags index — no LLM involved, same as any compiled-index
// language server.
func (s *Server) definition(
  ctx context.Context, id json.RawMessage, p DefinitionParams,
) {
  text := s.doc(p.TextDocument.URI)
  if text == "" {
    s.respond(id, []Location{}, nil)
    return
  }

  _, lineText := contextWindow(text, p.Position.Line, 0)
  word := wordAt(lineText, p.Position.Character)
  if word == "" {
    s.respond(id, []Location{}, nil)
    return
  }

  matches := s.symbols.Lookup(ctx, word, s.relPath(p.TextDocument.URI))
  locations := make([]Location, 0, len(matches))
  for _, m := range matches {
    locations = append(locations, Location{
      URI: s.fileURI(m.File), Range: pointRange(m.Line),
    })
  }
  s.respond(id, locations, nil)
}

// workspaceSymbol answers workspace/symbol from the same index —
// fuzzy-ish substring matching, also entirely deterministic.
func (s *Server) workspaceSymbol(
  ctx context.Context, id json.RawMessage, p WorkspaceSymbolParams,
) {
  matches := s.symbols.Search(ctx, p.Query)
  if len(matches) > maxWorkspaceSymbolResults {
    matches = matches[:maxWorkspaceSymbolResults]
  }

  results := make([]SymbolInformation, 0, len(matches))
  for _, m := range matches {
    results = append(results, SymbolInformation{
      Name: m.Name,
      Kind: symbolKindFor(m.Kind),
      Location: Location{
        URI: s.fileURI(m.File), Range: pointRange(m.Line),
      },
      ContainerName: m.Scope,
    })
  }
  s.respond(id, results, nil)
}

// symbolFact renders a short, deterministic clause about word's
// known definition, to ground a hover answer in a fact instead of
// a guess. Returns "" if word is empty or unknown to the index.
func (s *Server) symbolFact(ctx context.Context, word, uri string) string {
  if word == "" {
    return ""
  }

  matches := s.symbols.Lookup(ctx, word, s.relPath(uri))
  if len(matches) == 0 {
    return ""
  }

  m := matches[0]
  fact := fmt.Sprintf(
    " (%s %q is defined at %s:%d",
    ctags.KindLabel(m.Kind), word, m.File, m.Line,
  )
  if m.Scope != "" {
    fact += ", " + m.Scope
  }
  return fact + ")"
}

func (s *Server) fileURI(relPath string) string {
  return "file://" + filepath.Join(s.sandbox.Root, relPath)
}

// pointRange turns a 1-indexed ctags line number into a zero-width,
// 0-indexed LSP range at that line's start — armin doesn't have
// precise column/end information, and every editor jumps to
// Range.Start anyway.
func pointRange(line int) Range {
  l := line - 1
  if l < 0 {
    l = 0
  }
  point := Position{ Line: l, Character: 0 }
  return Range{ Start: point, End: point }
}

// wordAt extracts the maximal identifier ([letters/digits/_],
// Unicode-aware) that character sits directly on, or "" if it
// doesn't. This requires the character AT the cursor to itself be
// an identifier character — not just adjacent to one — matching how
// Neovim reports hover/definition positions: the column the cursor
// block is actually sitting on in normal mode, not a gap between
// characters. A cursor on "(" or a space has no word, even with an
// identifier right next to it.
func wordAt(line string, character int) string {
  isIdent := func(r rune) bool {
    return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
  }

  runes := []rune(line)
  outOfBounds := character < 0 || character >= len(runes)
  if outOfBounds || !isIdent(runes[character]) {
    return ""
  }

  start := character
  for start > 0 && isIdent(runes[start - 1]) {
    start--
  }
  end := character + 1
  for end < len(runes) && isIdent(runes[end]) {
    end++
  }

  return string(runes[start:end])
}

// symbolKinds maps a ctags kind letter to the LSP SymbolKind enum.
// Letters are language-specific and this list is not exhaustive;
// symbolKindFor falls back to Variable, a safe generic bucket, for
// anything unmapped.
var symbolKinds = map[string]int{
  "f": 12, // Function
  "m": 6,  // Method
  "s": 23, // Struct
  "c": 5,  // Class
  "v": 13, // Variable
  "p": 4,  // Package
  "i": 11, // Interface
  "t": 5,  // Type (treated as Class, the closest generic bucket)
  "e": 22, // EnumMember
  "g": 10, // Enum
  "d": 14, // Macro (treated as Constant)
  "n": 3,  // Namespace
  "l": 13, // Local variable
  "u": 23, // Union (treated as Struct)
  "C": 14, // Constant
}

func symbolKindFor(kind string) int {
  if k, ok := symbolKinds[kind]; ok {
    return k
  }
  return 13 // Variable
}
