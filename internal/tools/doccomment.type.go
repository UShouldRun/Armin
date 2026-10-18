// Inputs and outputs of doccomment.go's tools.

package tools

type ReadDocCommentInput struct {
  Name     string `json:"name"`
  FromFile string `json:"from_file,omitempty"`
}

type ReadDocCommentOutput struct {
  File    string `json:"file"`
  Line    int    `json:"line"`
  Comment string `json:"comment"` // empty if no doc comment precedes the definition
}
