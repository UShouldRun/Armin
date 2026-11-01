// The types of repomap.go.

package repomap

import "armin/internal/ctags"

// def is one definition and how often the rest of the project names
// it.
type def struct {
  sym   ctags.Symbol
  score float64
}
