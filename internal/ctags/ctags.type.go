// The types of ctags.go.

package ctags

import (
  "sync"
  "time"
)

// Symbol is one definition site, as reported by ctags.
type Symbol struct {
  Name string
  Kind string // single-letter ctags kind, language-dependent
  File string // relative to the index's root
  Line int
  // Scope is e.g. "struct:agent.Loop" or "package:agent"; "" if none.
  Scope string
}

// Index is a lazily-built, TTL-cached, name -> definitions map for
// one project root. Safe for concurrent use.
type Index struct {
  root, binary string

  mu      sync.Mutex
  byName  map[string][]Symbol
  builtAt time.Time
  lastErr error
}
