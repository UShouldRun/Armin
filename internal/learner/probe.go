// Finding what the user knows of a set of concepts with few questions.
// On a chain of prerequisites this is bisection (about log2 n
// questions); on a DAG the next question goes to the concept whose
// answer settles the most either way: right settles it and everything
// it requires, wrong settles it and everything that requires it.

package learner

import (
  "math/bits"
  "time"

  "armin/internal/know"
)

// NewProbe prepares to probe nodes, using the requires edges among
// them (at any depth through other nodes of the set).
func (l *Learner) NewProbe(nodes []know.NodeID) *Probe {
  n := len(nodes)
  words := (n + 63) / 64
  p := &Probe{
    nodes:   nodes,
    index:   make(map[know.NodeID]int, n),
    below:   make([][]uint64, n),
    above:   make([][]uint64, n),
    settled: make([]uint64, words),
    known:   make([]uint64, words),
  }

  for i, id := range nodes {
    p.index[id] = i
    p.below[i] = make([]uint64, words)
    p.above[i] = make([]uint64, words)
  }

  // below: a search from each node along requires, recording members
  var edges []know.Edge
  for i, id := range nodes {
    seen := map[know.NodeID]bool{ id: true }
    queue := []know.NodeID{ id }
    for len(queue) > 0 {
      cur := queue[0]
      queue = queue[1:]
      edges = l.Tree.Out(cur, edges[:0])
      for _, e := range edges {
        if e.Kind != know.EdgeRequires || seen[e.To] {
          continue
        }

        seen[e.To] = true
        queue = append(queue, e.To)
        j, ok := p.index[e.To]
        if ok {
          set(p.below[i], j)
          set(p.above[j], i)
        }
      }
    }
  }

  return p
}

func set(b []uint64, i int)      { b[i / 64] |= 1 << (i % 64) }
func isSet(b []uint64, i int) bool { return b[i / 64] & (1 << (i % 64)) != 0 }

// open counts the unsettled members of row.
func (p *Probe) open(row []uint64) int {
  n := 0
  for w, v := range row {
    n += bits.OnesCount64(v &^ p.settled[w])
  }

  return n
}

// Next is the concept to ask about, or false when all are settled.
func (p *Probe) Next() (know.NodeID, bool) {
  best, bestMin, bestSum := -1, -1, -1
  for i := range p.nodes {
    if isSet(p.settled, i) {
      continue
    }

    right := p.open(p.below[i]) + 1 // settled by a right answer
    wrong := p.open(p.above[i]) + 1 // settled by a wrong one
    least := min(right, wrong)
    if least > bestMin || least == bestMin && right + wrong > bestSum {
      best, bestMin, bestSum = i, least, right + wrong
    }
  }

  if best < 0 {
    return 0, false
  }

  return p.nodes[best], true
}

// Answer settles id by the user's answer, and with it its
// prerequisites (right) or its dependents (wrong).
func (p *Probe) Answer(id know.NodeID, right bool) {
  i, ok := p.index[id]
  if !ok || isSet(p.settled, i) {
    return
  }

  set(p.settled, i)
  if right {
    set(p.known, i)
  }

  row := p.above[i]
  if right {
    row = p.below[i]
  }

  for j := range p.nodes {
    if !isSet(row, j) || isSet(p.settled, j) {
      continue
    }

    set(p.settled, j)
    if right {
      set(p.known, j)
    }
  }
}

// Known reports what the probe concluded about id: known, and whether
// it is settled at all.
func (p *Probe) Known(id know.NodeID) (bool, bool) {
  i, ok := p.index[id]
  if !ok || !isSet(p.settled, i) {
    return false, false
  }

  return isSet(p.known, i), true
}

// AreaReport records the user's self-report on a top-level area. The
// two ends settle the area at once: "never heard" leaves its concepts
// unknown, "could explain" makes them known by implication. The middle
// ones return the area's concepts, for a Probe.
func (l *Learner) AreaReport(
  area know.NodeID, level Result, sure Sure, when time.Time,
) ([]know.NodeID, error) {
  err := l.Record(area, EventSelfReport, level, sure, when)
  if err != nil {
    return nil, err
  }

  var under []know.NodeID
  stack := l.Tree.Children(area, nil)
  for len(stack) > 0 {
    n := stack[len(stack) - 1]
    stack = stack[:len(stack) - 1]
    under = append(under, n)
    stack = l.Tree.Children(n, stack)
  }

  switch level {
  case ReportNever:
    return nil, nil
  case ReportExplain:
    day := Day(when)
    for _, n := range under {
      s := l.at(n)
      if !s.has() {
        *s = (hasBit | impliedBit).withDay(day)
      }
    }

    return nil, nil
  }

  return under, nil
}
