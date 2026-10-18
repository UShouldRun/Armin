// WebSearch: a query through the local SearXNG (internal/searxng).

package tools

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/searxng"
)

const (
  defaultResultCount = 5
  maxResultCount     = 10
)

// WebSearch queries the local SearXNG instance mgr manages, starting
// it on first use. A nil mgr (Docker wasn't found at startup — see
// cmd/armin/main.go) is reported as a clean, actionable error
// instead of a nil-pointer panic.
func WebSearch(
  ctx context.Context, mgr *searxng.Manager, in WebSearchInput,
) (*WebSearchOutput, error) {
  if mgr == nil {
    return nil, fmt.Errorf(
      "\n\t... in WebSearch:\n\t web search is unavailable — docker " +
        "wasn't found on this machine when armin started",
    )
  }
  if strings.TrimSpace(in.Query) == "" {
    return nil, fmt.Errorf("\n\t... in WebSearch:\n\t query is required")
  }

  count := in.Count
  switch {
  case count <= 0:
    count = defaultResultCount
  case count > maxResultCount:
    count = maxResultCount
  }

  results, err := mgr.Search(ctx, in.Query, count, in.Topic)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in WebSearch:\n\t%w", err)
  }

  out := make([]WebSearchResult, len(results))
  for i, r := range results {
    out[i] = WebSearchResult{ Title: r.Title, URL: r.URL, Snippet: r.Snippet }
  }
  return &WebSearchOutput{ Results: out }, nil
}
