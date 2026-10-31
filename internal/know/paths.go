// (parent, name) → node lookup with an open-addressing table, and the
// children index used by outlines and dumps.

package know

func pathHash(parent NodeID, name []byte) uint32 {
  h := uint32(2166136261)
  for i := 0; i < 4; i++ {
    h ^= uint32(parent >> (8 * i)) & 0xff
    h *= 16777619
  }

  for _, c := range name {
    h ^= uint32(c)
    h *= 16777619
  }

  return h
}

// Child finds the live child of parent called name.
func (g *Graph) Child(parent NodeID, name string) (NodeID, bool) {
  p := &g.paths
  if len(p.slots) == 0 {
    return 0, false
  }

  mask := uint32(len(p.slots) - 1)
  h := pathHash(parent, []byte(name))
  for i := h & mask; ; i = (i + 1) & mask {
    s := p.slots[i]
    if s.id == 0 {
      return 0, false
    }

    if s.hash != h {
      continue
    }

    id := NodeID(s.id - 1)
    n := g.node(id)
    if n.Parent == parent && string(g.Bytes(n.Name)) == name {
      return id, true
    }
  }
}

func (g *Graph) pathInsert(id NodeID) {
  p := &g.paths
  if (p.used + 1) * 2 > len(p.slots) {
    g.pathRebuild(p.used + 1)
  }

  n := g.node(id)
  h := pathHash(n.Parent, g.Bytes(n.Name))
  mask := uint32(len(p.slots) - 1)
  i := h & mask
  for p.slots[i].id != 0 {
    i = (i + 1) & mask
  }

  p.slots[i] = pathSlot{ hash: h, id: uint32(id) + 1 }
  p.used++
  p.dirty = true
}

// pathRemove deletes id's slot, shifting later entries of the probe
// run back so no tombstone is needed.
func (g *Graph) pathRemove(id NodeID) {
  p := &g.paths
  p.dirty = true
  if len(p.slots) == 0 {
    return
  }

  mask := uint32(len(p.slots) - 1)
  n := g.node(id)
  i := pathHash(n.Parent, g.Bytes(n.Name)) & mask
  for p.slots[i].id != uint32(id) + 1 {
    if p.slots[i].id == 0 {
      return
    }

    i = (i + 1) & mask
  }

  for j := (i + 1) & mask; p.slots[j].id != 0; j = (j + 1) & mask {
    home := p.slots[j].hash & mask
    // j's entry may move into the hole at i unless its home lies in
    // the cyclic range (i, j]
    if (j > i && (home <= i || home > j)) ||
      (j < i && home <= i && home > j) {
      p.slots[i] = p.slots[j]
      i = j
    }
  }

  p.slots[i] = pathSlot{}
  p.used--
}

// pathRebuild sizes the table for at least want entries and inserts
// every live non-root node.
func (g *Graph) pathRebuild(want int) {
  p := &g.paths
  size := 16
  for size < want * 2 {
    size <<= 1
  }

  p.slots = make([]pathSlot, size)
  p.used = 0
  mask := uint32(size - 1)
  for id := 1; id < g.Len(); id++ {
    n := g.node(NodeID(id))
    if n.Flags & FlagDeleted != 0 {
      continue
    }

    h := pathHash(n.Parent, g.Bytes(n.Name))
    i := h & mask
    for p.slots[i].id != 0 {
      i = (i + 1) & mask
    }

    p.slots[i] = pathSlot{ hash: h, id: uint32(id) + 1 }
    p.used++
  }

  p.dirty = true
}

// Children appends the live children of id to buf, in ID order.
func (g *Graph) Children(id NodeID, buf []NodeID) []NodeID {
  p := &g.paths
  if p.dirty || len(p.koffs) != g.Len() + 1 {
    g.kidsRebuild()
  }

  for _, k := range p.kids[p.koffs[id]:p.koffs[id + 1]] {
    buf = append(buf, NodeID(k))
  }

  return buf
}

// kidsRebuild is a counting sort of live nodes by parent: O(nodes).
func (g *Graph) kidsRebuild() {
  p := &g.paths
  n := g.Len()
  koffs := make([]uint32, n + 1)
  for id := 1; id < n; id++ {
    nd := g.node(NodeID(id))
    if nd.Flags & FlagDeleted == 0 {
      koffs[nd.Parent + 1]++
    }
  }

  for i := 1; i <= n; i++ {
    koffs[i] += koffs[i - 1]
  }

  kids := make([]uint32, koffs[n])
  fill := make([]uint32, n)
  copy(fill, koffs[:n])
  for id := 1; id < n; id++ {
    nd := g.node(NodeID(id))
    if nd.Flags & FlagDeleted == 0 {
      kids[fill[nd.Parent]] = uint32(id)
      fill[nd.Parent]++
    }
  }

  p.koffs, p.kids, p.dirty = koffs, kids, false
}
