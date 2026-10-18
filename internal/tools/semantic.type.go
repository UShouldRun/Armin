// Inputs and outputs of semantic.go's tools.

package tools

type SemanticSearchInput struct {
  Query string `json:"query"`
  Count int    `json:"count,omitempty"` // default 5, max 15
}

type SemanticSearchResult struct {
  File    string `json:"file"`
  Score   int    `json:"score"`
  Snippet string `json:"snippet"`
}

type SemanticSearchOutput struct {
  Results []SemanticSearchResult `json:"results"`
}
