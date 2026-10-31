// The project's knowledge tree and the user's learner tree: opened with
// the agent loop, compacted and closed at shutdown.

package think

import (
  "context"
  "fmt"
  "path/filepath"

  "armin/internal/know"
  "armin/internal/learner"
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

// closeKnow compacts the trees the session changed and closes them;
// once, since Shutdown runs from both a signal and the normal exit,
// and never in the middle of a know tool's write.
func (m *Mind) closeKnow() {
  if m.Loop == nil {
    return
  }

  m.knowOnce.Do(func() {
    m.Loop.LockKnow()
    defer m.Loop.UnlockKnow()

    if m.Know != nil {
      closeTree(m.Know, "knowledge")
    }

    if m.Learner != nil {
      if m.Learner.Tree.Changed() {
        err := m.Learner.Tree.Compact()
        if err != nil {
          ui.Error("compacting the learner tree: %s", err)
        }
      }

      err := m.Learner.Close()
      if err != nil {
        ui.Error("closing the learner tree: %s", err)
      }
    }
  })
}

// closeTree compacts g if the session changed it, then closes it.
func closeTree(g *know.Graph, what string) {
  if g.Changed() {
    err := g.Compact()
    if err != nil {
      ui.Error("compacting the %s tree: %s", what, err)
    }
  }

  err := g.Close()
  if err != nil {
    ui.Error("closing the %s tree: %s", what, err)
  }
}

// openLearner opens the user's learner tree, <data>/armin/learner.
func openLearner() (*learner.Learner, error) {
  data, err := session.DataDir()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in openLearner:\n\t%w", err)
  }

  l, err := learner.Open(filepath.Join(data, "learner"))
  if err != nil {
    return nil, fmt.Errorf("\n\t... in openLearner:\n\t%w", err)
  }

  return l, nil
}

// consolidatePerSession caps the pointers consolidated at one start.
const consolidatePerSession = 5

// startConsolidation runs the consolidation pass in the background
// (agent.Consolidate): with a minion to do it and a tree it may write,
// it turns a few recorded pointers into claims while the user works.
func (m *Mind) startConsolidation() {
  loop := m.Loop
  if loop == nil || loop.Minion == nil || loop.Know == nil ||
    loop.Know.ReadOnly() {
    return
  }

  ctx, cancel := context.WithCancel(context.Background())
  m.stopConsolidating = cancel
  m.consolidating.Add(1)
  go func() {
    defer m.consolidating.Done()
    loop.Consolidate(ctx, consolidatePerSession)
  }()
}

// stopConsolidation cancels the pass and waits for it, so it never
// writes to a closed tree.
func (m *Mind) stopConsolidation() {
  if m.stopConsolidating == nil {
    return
  }

  m.stopConsolidating()
  m.consolidating.Wait()
}

