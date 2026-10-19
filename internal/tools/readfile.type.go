// Inputs and outputs of readfile.go's tools.

package tools

// ReadFileInput is the structured argument set for read_file.
type ReadFileInput struct {
  Path      string `json:"path"`
  StartLine int    `json:"start_line,omitempty"` // 1-indexed, inclusive
  EndLine   int    `json:"end_line,omitempty"`   // 1-indexed, inclusive
}

type ReadFileOutput struct {
  Content    string `json:"content"`
  TotalLines int    `json:"total_lines"`
  Truncated  bool   `json:"truncated,omitempty"`
  NextLine   int    `json:"next_line,omitempty"` // where a cut read resumes
}
