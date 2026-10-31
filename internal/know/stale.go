// Keeping the tree honest when the code changes. The caller (think)
// finds which files changed and reads them; this side:
//
//   CitedFiles   the files file evidence points at, for that check
//   CheckFile    re-hash a changed file's cited ranges: unchanged,
//                moved within the file (re-anchored), or stale
//   MarkSuspect  breadth first over reverse requires and derived-from
//                edges: everything resting on a stale node
//   FlagCycles   keep one question node per open requires loop, and
//                drop the ones whose loop was resolved
//
// Cost: O(changed evidence + the lines of the changed files + the
// affected subgraph).

package know

import (
  "fmt"
  "path/filepath"
  "slices"
  "strings"
)

// CitedFiles maps each file cited by live file evidence (as its
// cleaned path) to that evidence.
func (g *Graph) CitedFiles() map[string][]EvidenceID {
  out := map[string][]EvidenceID{}
  for i := 0; i < g.EvidenceCount(); i++ {
    e := g.Ev(EvidenceID(i))
    if e.Kind != RefFile || !g.live(e.Node) {
      continue
    }

    path, _, _, ok := ParseRange(g.Str(e.Ref))
    if ok {
      out[path] = append(out[path], EvidenceID(i))
    }
  }

  return out
}

// ParseRange reads "path:12-40" (or "path:12") into a cleaned path and
// 1-based inclusive lines.
func ParseRange(ref string) (string, int, int, bool) {
  i := strings.LastIndexByte(ref, ':')
  if i <= 0 {
    return "", 0, 0, false
  }

  a, b, found := strings.Cut(ref[i + 1:], "-")
  if !found {
    b = a
  }

  var start, end int
  _, err1 := fmt.Sscanf(a, "%d", &start)
  _, err2 := fmt.Sscanf(b, "%d", &end)
  if err1 != nil || err2 != nil || start < 1 || end < start ||
    fmt.Sprint(start) != a || fmt.Sprint(end) != b {
    return "", 0, 0, false
  }

  return filepath.Clean(ref[:i]), start, end, true
}

// CheckFile compares the evidence citing path with its current
// content (nil when the file is gone). A range whose lines still hash
// the same is fine; one whose lines moved within the file is pointed
// at their new place; the rest make their nodes stale. It returns the
// nodes newly marked stale and how many references moved.
func (g *Graph) CheckFile(
  path string, data []byte, gone bool, evs []EvidenceID,
) ([]NodeID, int, error) {
  var lines []uint32
  if !gone {
    lines = LineHashes(data, nil)
  }

  var stale []NodeID
  moved := 0
  for _, id := range evs {
    e := g.Ev(id)
    _, start, end, ok := ParseRange(g.Str(e.Ref))
    if !ok || e.Kind != RefFile {
      continue
    }

    n := end - start + 1
    if !gone && end <= len(lines) && RangeHash(lines[start - 1:end]) == e.Hash {
      continue
    }

    at := -1
    if !gone {
      at = Roll(lines, n, e.Hash, start - 1)
    }

    if at >= 0 {
      ref := fmt.Sprintf("%s:%d-%d", path, at + 1, at + n)
      err := g.Reanchor(id, ref, e.Hash)
      if err != nil {
        return stale, moved, err
      }

      moved++
      continue
    }

    if g.node(e.Node).Flags & FlagStale != 0 {
      continue
    }

    err := g.SetFlags(e.Node, FlagStale, 0)
    if err != nil {
      return stale, moved, err
    }

    stale = append(stale, e.Node)
  }

  return stale, moved, nil
}

// MarkSuspect flags as suspect every live node that requires or was
// derived from one of from, directly or not, and isn't stale or
// suspect already. It returns how many it marked.
func (g *Graph) MarkSuspect(from []NodeID) (int, error) {
  seen := make([]uint64, (g.Len() + 63) / 64)
  mark := func(id NodeID) bool {
    w, b := id / 64, uint64(1) << (id % 64)
    if seen[w] & b != 0 {
      return false
    }

    seen[w] |= b

    return true
  }

  queue := slices.Clone(from)
  for _, id := range from {
    mark(id)
  }

  count := 0
  var in []Edge
  for len(queue) > 0 {
    id := queue[0]
    queue = queue[1:]
    in = g.In(id, in[:0])
    for _, e := range in {
      if e.Kind != EdgeRequires && e.Kind != EdgeDerivedFrom {
        continue
      }

      if !g.live(e.To) || !mark(e.To) {
        continue
      }

      queue = append(queue, e.To)
      if g.node(e.To).Flags & (FlagStale | FlagSuspect) != 0 {
        continue
      }

      err := g.SetFlags(e.To, FlagSuspect, 0)
      if err != nil {
        return count, err
      }

      count++
    }
  }

  return count, nil
}

// cyclesPath is where the question nodes for open loops live.
const cyclesPath = "cycles"

// FlagCycles runs the cycle pass and keeps the tree's record of it in
// step: each open requires loop gets a question node under cycles/
// (flagged cycle, an explains edge to every member), and a question
// whose loop no longer exists is deleted. It returns the open loops.
func (g *Graph) FlagCycles(author AuthorID) ([][]NodeID, error) {
  loops := g.Cycles()
  open := map[string]bool{}
  for _, l := range loops {
    open[loopName(l)] = true
  }

  dir, ok := g.Lookup(cyclesPath)
  if ok {
    for _, q := range g.Children(dir, nil) {
      if g.node(q).Flags & FlagCycle == 0 {
        continue
      }

      name := g.Name(q)
      if open[name] {
        delete(open, name) // already recorded
        continue
      }

      err := g.Delete(q)
      if err != nil {
        return loops, err
      }
    }
  }

  for _, l := range loops {
    name := loopName(l)
    if !open[name] {
      continue
    }

    if !ok {
      var err error
      dir,
      err = g.AddNode(0, cyclesPath, "requires loops waiting to be resolved",
        KindTopic, ConfChecked, author)
      if err != nil {
        return loops, err
      }

      ok = true
    }

    var paths []string
    for _, id := range l {
      paths = append(paths, fmt.Sprintf("n%d %s", id, g.Path(id)))
    }

    claim := "these require each other in a loop: " + strings.Join(
      paths, ", ",
    ) +
      ". Resolve it: merge them into one node, flip the wrong edge, or " +
      "make a mutual dependency explains"
    q, err := g.AddNode(dir, name, claim, KindQuestion, ConfChecked, author)
    if err != nil {
      return loops, err
    }

    err = g.SetFlags(q, FlagCycle, 0)
    if err != nil {
      return loops, err
    }

    for _, id := range l {
      err = g.AddEdge(q, id, EdgeExplains)
      if err != nil {
        return loops, err
      }
    }
  }

  if ok && len(g.Children(dir, nil)) == 0 {
    err := g.Delete(dir)
    if err != nil {
      return loops, err
    }
  }

  return loops, nil
}

// loopName names a loop by its members, loop-3-7-9, or past eight
// of them by the first and the count, loop-3-and-41-more.
func loopName(l []NodeID) string {
  if len(l) > 8 {
    return fmt.Sprintf("loop-%d-and-%d-more", l[0], len(l) - 1)
  }

  var b strings.Builder
  b.WriteString("loop")
  for _, id := range l {
    fmt.Fprintf(&b, "-%d", id)
  }

  return b.String()
}
