// The types of edges.go.

package know

// edges holds a tree's edges as CSR arrays: offs[id] .. offs[id + 1]
// is the range of node id's targets in tgts, each target packing the
// node ID in its low 29 bits and the edge kind in the top 3. The out
// arrays come from the snapshot; the in arrays are built at load.
// Edges added since go to delta until it is folded into new arrays.
type edges struct {
  offs, tgts   []uint32
  roffs, rtgts []uint32
  delta        []rawEdge
}

// rawEdge is one delta entry: the source and the packed target.
type rawEdge struct {
  from, tgt uint32
}
