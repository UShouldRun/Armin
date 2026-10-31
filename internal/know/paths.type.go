// The types of paths.go.

package know

// paths finds a node by (parent, name) and lists a node's children.
// Neither is stored: both are rebuilt from the node array at load.
type paths struct {
  // slots is an open-addressing table (linear probing, power-of-two
  // size, at most half full) of live non-root nodes.
  slots []pathSlot
  used  int

  // koffs and kids are a CSR of children by parent, rebuilt on the
  // first Children call after a node was added, moved or deleted.
  koffs, kids []uint32
  dirty       bool
}

// pathSlot is 8 bytes: the hash of (parent, name), kept to skip most
// string compares, and the node ID plus one (0 is an empty slot).
type pathSlot struct {
  hash, id uint32
}
