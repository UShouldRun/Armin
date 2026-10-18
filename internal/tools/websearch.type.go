// Inputs and outputs of websearch.go's tools.

package tools

// WebSearchInput is the argument set for web_search.
type WebSearchInput struct {
  Query string `json:"query"`
  Count int    `json:"count,omitempty"` // default 5, max 10
  Topic string `json:"topic,omitempty"` // general (default), code, science
}

type WebSearchResult struct {
  ID      int    `json:"id,omitempty"` // citation number, set by the agent
  Title   string `json:"title"`
  URL     string `json:"url"`
  Snippet string `json:"snippet"`
}

type WebSearchOutput struct {
  Results []WebSearchResult `json:"results"`
  Cite    string            `json:"cite,omitempty"`
}
