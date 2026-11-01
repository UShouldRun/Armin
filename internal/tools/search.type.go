// Inputs and outputs of search.go's tools.

package tools

// SearchCodeInput is the structured argument set for search_code — a
// pattern and structured filters, never a free-form shell string.
type SearchCodeInput struct {
  Pattern  string `json:"pattern"`
  Path     string `json:"path,omitempty"`      // default: project root
  FileType string `json:"file_type,omitempty"` // e.g. "go" — passed to rg -t
  Context  int    `json:"context,omitempty"`   // lines around a match (2)
  Literal  bool   `json:"literal,omitempty"`   // the pattern as plain text
}

type SearchCodeOutput struct {
  Matches   string `json:"matches"`
  Truncated bool   `json:"truncated,omitempty"`
}
