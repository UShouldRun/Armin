// Out and in edges: CSR arrays for what the snapshot held, a short
// delta list for what was added since, folded back into CSR once it
// passes foldAt. A removed CSR edge becomes a tombstone (deadEdge) in
// place until the next fold.

package know

// foldAt is how long the delta grows before it is folded into CSR;
// below it a scan of the delta is cheaper than a rebuild.
const foldAt = 1024

const deadEdge = 0xffffffff

func pack(to NodeID, k EdgeKind) uint32 {
  return uint32(to) | uint32(k) << 29
}

func unpack(t uint32) Edge {
  return Edge{ To: NodeID(t & (maxNodes - 1)), Kind: EdgeKind(t >> 29) }
}

// csrRange returns node id's slice of a CSR, or nil when id is past it.
func csrRange(offs, tgts []uint32, id NodeID) []uint32 {
  if int(id) + 1 >= len(offs) {
    return nil
  }

  return tgts[offs[id]:offs[id + 1]]
}

// Out appends node id's outgoing edges to buf.
func (g *Graph) Out(id NodeID, buf []Edge) []Edge {
  e := &g.edges
  for _, t := range csrRange(e.offs, e.tgts, id) {
    if t != deadEdge {
      buf = append(buf, unpack(t))
    }
  }

  for _, d := range e.delta {
    if d.from == uint32(id) {
      buf = append(buf, unpack(d.tgt))
    }
  }

  return buf
}

// In appends node id's incoming edges to buf; each Edge.To is the
// source.
func (g *Graph) In(id NodeID, buf []Edge) []Edge {
  e := &g.edges
  for _, t := range csrRange(e.roffs, e.rtgts, id) {
    if t != deadEdge {
      buf = append(buf, unpack(t))
    }
  }

  for _, d := range e.delta {
    to := unpack(d.tgt)
    if to.To == id {
      buf = append(buf, Edge{ To: NodeID(d.from), Kind: to.Kind })
    }
  }

  return buf
}

// HasEdge reports whether from → to of kind exists.
func (g *Graph) HasEdge(from, to NodeID, kind EdgeKind) bool {
  e := &g.edges
  want := pack(to, kind)
  for _, t := range csrRange(e.offs, e.tgts, from) {
    if t == want {
      return true
    }
  }

  for _, d := range e.delta {
    if d.from == uint32(from) && d.tgt == want {
      return true
    }
  }

  return false
}

func (e *edges) add(from uint32, tgt uint32, nodes int) {
  e.delta = append(e.delta, rawEdge{ from: from, tgt: tgt })
  if len(e.delta) > foldAt {
    e.fold(nodes)
  }
}

func (e *edges) remove(from uint32, tgt uint32) {
  for i, d := range e.delta {
    if d.from == from && d.tgt == tgt {
      last := len(e.delta) - 1
      e.delta[i] = e.delta[last]
      e.delta = e.delta[:last]
      return
    }
  }

  out := csrRange(e.offs, e.tgts, NodeID(from))
  for i, t := range out {
    if t == tgt {
      out[i] = deadEdge
      break
    }
  }

  to := unpack(tgt)
  back := pack(NodeID(from), to.Kind)
  in := csrRange(e.roffs, e.rtgts, to.To)
  for i, t := range in {
    if t == back {
      in[i] = deadEdge
      break
    }
  }
}

// count is the number of live edges.
func (e *edges) count() int {
  n := len(e.delta)
  for _, t := range e.tgts {
    if t != deadEdge {
      n++
    }
  }

  return n
}

// fold merges the delta and drops tombstones into fresh heap CSR
// arrays over nodes IDs, then rebuilds the in arrays: O(nodes + edges).
func (e *edges) fold(nodes int) {
  offs := make([]uint32, nodes + 1)
  for id := 0; id < nodes; id++ {
    for _, t := range csrRange(e.offs, e.tgts, NodeID(id)) {
      if t != deadEdge {
        offs[id + 1]++
      }
    }
  }

  for _, d := range e.delta {
    offs[d.from + 1]++
  }

  for i := 1; i <= nodes; i++ {
    offs[i] += offs[i - 1]
  }

  tgts := make([]uint32, offs[nodes])
  fill := make([]uint32, nodes)
  copy(fill, offs[:nodes])
  for id := 0; id < nodes; id++ {
    for _, t := range csrRange(e.offs, e.tgts, NodeID(id)) {
      if t != deadEdge {
        tgts[fill[id]] = t
        fill[id]++
      }
    }
  }

  for _, d := range e.delta {
    tgts[fill[d.from]] = d.tgt
    fill[d.from]++
  }

  e.offs, e.tgts, e.delta = offs, tgts, e.delta[:0]
  e.reverse(nodes)
}

// reverse builds the in arrays from the out arrays by counting sort.
func (e *edges) reverse(nodes int) {
  roffs := make([]uint32, nodes + 1)
  for _, t := range e.tgts {
    if t != deadEdge {
      roffs[unpack(t).To + 1]++
    }
  }

  for i := 1; i <= nodes; i++ {
    roffs[i] += roffs[i - 1]
  }

  rtgts := make([]uint32, roffs[nodes])
  fill := make([]uint32, nodes)
  copy(fill, roffs[:nodes])
  for from := 0; from + 1 < len(e.offs) && from < nodes; from++ {
    for _, t := range e.tgts[e.offs[from]:e.offs[from + 1]] {
      if t == deadEdge {
        continue
      }

      to := unpack(t)
      rtgts[fill[to.To]] = pack(NodeID(from), to.Kind)
      fill[to.To]++
    }
  }

  e.roffs, e.rtgts = roffs, rtgts
}
