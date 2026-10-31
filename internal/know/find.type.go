// The types of find.go.

package know

// Hit is one search or similarity result.
type Hit struct {
  ID    NodeID
  Score float32
}

// index is the BM25 index over node names and claims, built on the
// first Find and patched in between rebuilds:
//
//   - terms are sorted (for prefix ranges by binary search) and stored
//     as offsets into one byte arena
//   - postings are a CSR per term, each entry the node ID in the low
//     29 bits and the term frequency (capped at 7) in the top 3
//   - nodes at or past built, and nodes in stale (text changed or
//     deleted since the build), are scored by scanning them directly
//
// Euler-tour intervals (tin, tout) answer "is a under b" in O(1) for
// the subtree filter; a move invalidates them.
type index struct {
  built int

  termText []byte
  termOff  []uint32 // len(terms) + 1 offsets into termText
  poffs    []uint32 // len(terms) + 1 offsets into post
  post     []uint32
  docLen   []uint8
  avgLen   float32

  stale  []uint64 // bitset over [0, built)
  nstale int

  tin, tout   []uint32
  eulerDirty  bool

  // query scratch, reused
  score   []float32
  touched []NodeID
  buf     []byte
}
