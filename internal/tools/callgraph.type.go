package tools

import (
  "sync"
  "time"
)

// CallSite is one place a function/method name is invoked.
type CallSite struct {
  File string `json:"file"`
  Line int    `json:"line"`
}

// callGraphData is one full-tree AST scan's result: byName maps a
// called identifier to every call site, enclosing maps a call site
// back to the name of the top-level function/method it's inside —
// built together in one pass (scanCalls) so impact_analysis's BFS
// doesn't need a second walk to step outward.
type callGraphData struct {
  byName    map[string][]CallSite
  enclosing map[string]string // "file:line" -> enclosing function name
}

// callGraphCache mirrors ctags.Index's short-TTL pattern: parsing
// every .go file is real work, and find_callers/find_dead_code/
// impact_analysis can all be called multiple times in one
// conversation turn.
type callGraphCache struct {
  mu      sync.Mutex
  root    string
  data    *callGraphData
  builtAt time.Time
}

type FindCallersInput struct {
  Name string `json:"name"`
}

type FindCallersOutput struct {
  Calls []CallSite `json:"calls"`
}

type FindDeadCodeInput struct {
  Path string `json:"path,omitempty"` // limit the exported-symbol scan to this file/dir prefix
}

type DeadCodeCandidate struct {
  Name string `json:"name"`
  File string `json:"file"`
  Line int    `json:"line"`
}

type FindDeadCodeOutput struct {
  Candidates []DeadCodeCandidate `json:"candidates"`
}

type ImpactAnalysisInput struct {
  Name  string `json:"name"`
  Depth int    `json:"depth,omitempty"` // default 2, max 3
}

type ImpactAnalysisOutput struct {
  Affected  []string `json:"affected"` // function names that transitively call Name
  Truncated bool     `json:"truncated,omitempty"`
}
