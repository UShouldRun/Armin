// The types of snap.go.

package know

// snapHeader is the first 96 bytes of graph.snap, little-endian.
// Every section starts on an 8-byte boundary, and the CRC covers
// everything after the header.
type snapHeader struct {
  Magic                [4]byte // "ARMK"
  Version              uint32
  LogGen               uint64 // the graph.log generation that extends it
  Nodes, Edges         uint32
  Evidence, Authors    uint32
  ArenaLen, CRC        uint32
  OffNodes, OffOffs    uint64
  OffTgts, OffEvidence uint64
  OffAuthors, OffArena uint64
  Size                 uint64
}
