package tools

// SymbolLocation is one ctags.Symbol, reshaped for the JSON contract
// tool results use (exported fields, json tags) rather than handing
// back internal/ctags' own type directly.
type SymbolLocation struct {
  Name  string `json:"name"`
  Kind  string `json:"kind"`
  File  string `json:"file"`
  Line  int    `json:"line"`
  Scope string `json:"scope,omitempty"`
}

type FindDefinitionInput struct {
  Name     string `json:"name"`
  FromFile string `json:"from_file,omitempty"` // ranks same-file/same-dir matches first
}

type FindDefinitionOutput struct {
  Matches []SymbolLocation `json:"matches"`
}

type ListSymbolsInput struct {
  Query string `json:"query,omitempty"` // substring, case-insensitive; empty lists everything (capped)
}

type ListSymbolsOutput struct {
  Matches   []SymbolLocation `json:"matches"`
  Truncated bool             `json:"truncated,omitempty"`
}

type OutlineFileInput struct {
  Path string `json:"path"`
}

type OutlineFileOutput struct {
  Symbols []SymbolLocation `json:"symbols"`
}

type FindReferencesInput struct {
  Name string `json:"name"`
  Path string `json:"path,omitempty"`
}

type FindReferencesOutput struct {
  Matches   string `json:"matches"`
  Truncated bool   `json:"truncated,omitempty"`
}
