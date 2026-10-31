// The project's knowledge tree: opened with the agent loop, compacted
// and closed at shutdown.

package think

import (
  "fmt"

  "armin/internal/know"
  "armin/internal/session"
  "armin/internal/ui"
)

// OpenKnow opens the project's tree, for -know.
func OpenKnow(root string) (*know.Graph, error) {
  return openKnow(root)
}

// openKnow opens the tree under <data>/armin/know/<project key>.
// Another armin on the same project holding it gives a read-only
// tree: searches work, writes report it.
func openKnow(root string) (*know.Graph, error) {
  dir, err := session.ProjectDataDir("know", root)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in openKnow:\n\t%w", err)
  }

  g, err := know.Open(dir)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in openKnow:\n\t%w", err)
  }

  return g, nil
}

// closeKnow compacts a tree the session changed and closes it; once,
// since Shutdown runs from both a signal and the normal exit, and
// never in the middle of a know tool's write.
func (m *Mind) closeKnow() {
  if m.Know == nil {
    return
  }

  m.knowOnce.Do(func() {
    m.Loop.LockKnow()
    defer m.Loop.UnlockKnow()

    if m.Know.Changed() {
      err := m.Know.Compact()
      if err != nil {
        ui.Error("compacting the knowledge tree: %s", err)
      }
    }

    err := m.Know.Close()
    if err != nil {
      ui.Error("closing the knowledge tree: %s", err)
    }
  })
}
