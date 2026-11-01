// The algorithms over the graph besides search:
//
//   Similar   near-duplicates of a node about to be written (Jaccard)
//   Cycles    every loop of requires edges (Tarjan's SCC)
//   Trace     what a conclusion was derived from, weakest first
//   Plan      the prerequisites of a goal in teaching order (Kahn)

package know

import (
  "container/heap"
  "fmt"
  "math/bits"
  "slices"
  "strings"

  "armin/internal/tokens"
)

// SimilarAt is the Jaccard similarity from which a new node's write
// is held back for the model to decide: link, update, or add anyway.
const SimilarAt = 0.6

// termSet is the sorted, distinct terms of a text.
func termSet(text string, buf []byte, out []string) []string {
  out = out[:0]
  tokens.Each(text, buf, func(t []byte) { out = append(out, string(t)) })
  slices.Sort(out)

  return slices.Compact(out)
}

// jaccard of two sorted sets, by merging: O(a + b).
func jaccard(a, b []string) float32 {
  if len(a) == 0 && len(b) == 0 {
    return 0
  }

  both := 0
  for i, j := 0, 0; i < len(a) && j < len(b); {
    switch strings.Compare(a[i], b[j]) {
    case 0:
      both++
      i++
      j++
    case -1:
      i++
    default:
      j++
    }
  }

  return float32(both) / float32(len(a) + len(b) - both)
}

// Similar returns the live nodes whose name and claim overlap
// name + claim by at least SimilarAt, best first. The candidates are
// parent's children and the top 5 search hits, so no full scan.
func (g *Graph) Similar(parent NodeID, name, claim string) []Hit {
  text := name + " " + claim
  var cands []NodeID
  if g.live(parent) {
    cands = g.Children(parent, cands)
  }

  for _, h := range g.Find(text, 0, 5) {
    if !slices.Contains(cands, h.ID) {
      cands = append(cands, h.ID)
    }
  }

  buf := make([]byte, 0, 64)
  want := termSet(text, buf, nil)
  var have []string
  var out []Hit
  for _, id := range cands {
    have = termSet(g.Name(id) + " " + g.Claim(id), buf, have)
    s := jaccard(want, have)
    if s >= SimilarAt {
      out = append(out, Hit{ ID: id, Score: s })
    }
  }

  slices.SortFunc(
    out,
    func(a, b Hit) int {
      if a.Score != b.Score {
        if a.Score > b.Score {
          return -1
        }

        return 1
      }

      return int(a.ID) - int(b.ID)
    },
  )

  return out
}

// Cycles returns every loop of requires edges among live nodes, each
// a strongly connected component of two or more nodes, members in ID
// order: Tarjan's algorithm, iterative, O(nodes + edges).
func (g *Graph) Cycles() [][]NodeID {
  n := g.Len()
  const unseen = -1
  index := make([]int32, n)
  low := make([]int32, n)
  onStack := make([]bool, n)
  for i := range index {
    index[i] = unseen
  }

  // each frame's requires edges sit in slab[next:end]; slab only
  // grows, so the whole pass allocates O(edges) once
  type frame struct {
    id        NodeID
    next, end int
  }

  var out [][]NodeID
  var stack []NodeID
  var call []frame
  var slab []Edge
  var comp []NodeID
  clock := int32(0)
  push := func(id NodeID) {
    index[id], low[id] = clock, clock
    clock++
    stack = append(stack, id)
    onStack[id] = true
    start := len(slab)
    slab = g.Out(id, slab)
    k := start
    for _, e := range slab[start:] {
      if e.Kind == EdgeRequires && g.live(e.To) {
        slab[k] = e
        k++
      }
    }

    slab = slab[:k]
    call = append(call, frame{ id: id, next: start, end: k })
  }

  for root := 1; root < n; root++ {
    if index[root] != unseen || !g.live(NodeID(root)) {
      continue
    }

    push(NodeID(root))
    for len(call) > 0 {
      top := &call[len(call) - 1]
      if top.next < top.end {
        w := slab[top.next].To
        top.next++
        if index[w] == unseen {
          push(w)
        } else if onStack[w] {
          low[top.id] = min(low[top.id], index[w])
        }

        continue
      }

      v := top.id
      call = call[:len(call) - 1]
      if len(call) == 0 {
        slab = slab[:0]
      }

      if len(call) > 0 {
        p := call[len(call) - 1].id
        low[p] = min(low[p], low[v])
      }

      if low[v] != index[v] {
        continue
      }

      comp = comp[:0]
      for {
        w := stack[len(stack) - 1]
        stack = stack[:len(stack) - 1]
        onStack[w] = false
        comp = append(comp, w)
        if w == v {
          break
        }
      }

      if len(comp) > 1 {
        loop := slices.Clone(comp)
        slices.Sort(loop)
        out = append(out, loop)
      }
    }
  }

  return out
}

// Trace follows derived-from edges back from id, breadth first, to
// depth levels, and lists what it reached weakest first: unverified
// minion claims, then lower confidence, then stale, then by depth.
func (g *Graph) Trace(id NodeID, depth int) []TraceStep {
  if !g.live(id) {
    return nil
  }

  seen := map[NodeID]bool{ id: true }
  level := []NodeID{ id }
  var out []TraceStep
  var edges []Edge
  for d := 1; d <= depth && len(level) > 0; d++ {
    var next []NodeID
    for _, n := range level {
      edges = g.Out(n, edges[:0])
      for _, e := range edges {
        if e.Kind != EdgeDerivedFrom || seen[e.To] || !g.live(e.To) {
          continue
        }

        seen[e.To] = true
        next = append(next, e.To)
        out = append(out, TraceStep{ ID: e.To, Depth: d })
      }
    }

    level = next
  }

  weak := func(s TraceStep) (int, int, int) {
    n := g.node(s.ID)
    unverified := 1
    if g.authors[n.Author].Role == RoleMinion && n.Flags & FlagVerified == 0 {
      unverified = 0
    }

    stale := 1
    if n.Flags & (FlagStale | FlagSuspect) != 0 {
      stale = 0
    }

    return unverified, int(n.Conf()), stale
  }

  slices.SortStableFunc(
    out,
    func(a, b TraceStep) int {
      a1, a2, a3 := weak(a)
      b1, b2, b3 := weak(b)
      switch {
      case a1 != b1:
        return a1 - b1
      case a2 != b2:
        return a2 - b2
      case a3 != b3:
        return a3 - b3
      }

      return a.Depth - b.Depth
    },
  )

  return out
}

func (e *CycleError) Error() string {
  return fmt.Sprintf(
    "the prerequisites loop through %v; resolve the cycle first", e.Nodes,
  )
}

// Plan lists what must be learned for goal, prerequisites first: the
// requires closure of goal, stopping at nodes known reports true, in
// Kahn's order. Among the nodes ready at each step it takes the one
// that unlocks the most (most nodes in the closure that depend on it,
// directly or not), then the one with fewest prerequisites, then the
// lowest ID. frontier is how many were ready at the start: what can be
// taught now.
func (g *Graph) Plan(goal NodeID, known func(NodeID) bool) (
  order []NodeID, frontier int, err error,
) {
  if !g.live(goal) {
    return nil, 0, fmt.Errorf("\n\t... in Plan:\n\t%d: %w", goal, ErrNoNode)
  }

  // the closure: local indexes for every needed node
  local := map[NodeID]int{}
  var nodes []NodeID
  var prereq [][]int // local i requires local j
  var edges []Edge
  var visit func(NodeID) int
  visit = func(id NodeID) int {
    i, ok := local[id]
    if ok {
      return i
    }

    i = len(nodes)
    local[id] = i
    nodes = append(nodes, id)
    prereq = append(prereq, nil)
    edges = g.Out(id, edges[:0])
    var reqs []NodeID
    for _, e := range edges {
      if e.Kind == EdgeRequires && g.live(e.To) && !known(e.To) {
        reqs = append(reqs, e.To)
      }
    }

    for _, r := range reqs {
      j := visit(r)
      prereq[i] = append(prereq[i], j)
    }

    return i
  }

  if known(goal) {
    return nil, 0, nil
  }

  visit(goal)
  n := len(nodes)
  dependents := make([][]int, n)
  waiting := make([]int, n)
  for i, ps := range prereq {
    waiting[i] = len(ps)
    for _, j := range ps {
      dependents[j] = append(dependents[j], i)
    }
  }

  unlocks := g.unlockCounts(dependents)
  ready := &readyHeap{ unlocks: unlocks, prereq: prereq, nodes: nodes }
  for i := range nodes {
    if waiting[i] == 0 {
      ready.items = append(ready.items, i)
    }
  }

  heap.Init(ready)
  frontier = ready.Len()
  for ready.Len() > 0 {
    i := heap.Pop(ready).(int)
    order = append(order, nodes[i])
    for _, d := range dependents[i] {
      waiting[d]--
      if waiting[d] == 0 {
        heap.Push(ready, d)
      }
    }
  }

  if len(order) < n {
    var loop []NodeID
    for i, w := range waiting {
      if w > 0 {
        loop = append(loop, nodes[i])
      }
    }

    slices.Sort(loop)

    return nil, 0, &CycleError{ Nodes: loop }
  }

  return order, frontier, nil
}

// unlockCounts is, for each local node, how many others depend on it
// directly or not: bitset rows unioned along dependents in reverse
// topological order, O(n² / 64). Past 4096 nodes it falls back to
// direct dependents.
func (g *Graph) unlockCounts(dependents [][]int) []int {
  n := len(dependents)
  counts := make([]int, n)
  if n > 4096 {
    for i, d := range dependents {
      counts[i] = len(d)
    }

    return counts
  }

  words := (n + 63) / 64
  rows := make([]uint64, n * words)
  done := make([]bool, n)
  var fill func(int)
  fill = func(i int) {
    if done[i] {
      return
    }

    done[i] = true
    row := rows[i * words:(i + 1) * words]
    for _, d := range dependents[i] {
      fill(d)
      row[d / 64] |= 1 << (d % 64)
      for w, v := range rows[d * words:(d + 1) * words] {
        row[w] |= v
      }
    }
  }

  for i := range dependents {
    fill(i)
    for _, v := range rows[i * words:(i + 1) * words] {
      counts[i] += bits.OnesCount64(v)
    }
  }

  return counts
}

func (h *readyHeap) Len() int { return len(h.items) }

func (h *readyHeap) Less(a, b int) bool {
  i, j := h.items[a], h.items[b]
  if h.unlocks[i] != h.unlocks[j] {
    return h.unlocks[i] > h.unlocks[j]
  }

  if len(h.prereq[i]) != len(h.prereq[j]) {
    return len(h.prereq[i]) < len(h.prereq[j])
  }

  return h.nodes[i] < h.nodes[j]
}

func (h *readyHeap) Swap(a, b int) {
  h.items[a], h.items[b] = h.items[b], h.items[a]
}
func (h *readyHeap) Push(x any)    { h.items = append(h.items, x.(int)) }

func (h *readyHeap) Pop() any {
  last := h.items[len(h.items) - 1]
  h.items = h.items[:len(h.items) - 1]

  return last
}
