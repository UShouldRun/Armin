package searxng

import (
  "sync"
)

// Result is one search hit, already trimmed down to what's worth
// spending context on.
type Result struct {
  Title   string
  URL     string
  Snippet string
}

// Manager owns the container's lifecycle. Zero value is ready to
// use; construct with New.
type Manager struct {
  mu          sync.Mutex
  ready       bool
  startedByUs bool
}

// searxResponse is the slice of SearXNG's ?format=json body we
// actually use.
type searxResponse struct {
  Results []struct {
    URL     string `json:"url"`
    Title   string `json:"title"`
    Content string `json:"content"`
  } `json:"results"`
  // Unresponsive is [engine, reason] pairs for engines that failed.
  Unresponsive [][]string `json:"unresponsive_engines"`
}
