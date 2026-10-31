// The types of edit.go.

package know

// editBlock is one node block of an edited dump: the state the user
// wants that node in.
type editBlock struct {
  line       int
  id         NodeID // set for existing nodes, and for new ones once added
  isNew      bool
  path       string
  kind       Kind
  conf       Conf
  claim      string
  flags      NodeFlags
  edges      [EdgeDerivedFrom + 1][]string // targets: "n42" or a path
}

// EditResult counts what an edit changed.
type EditResult struct {
  Added, Changed, Deleted int
}
