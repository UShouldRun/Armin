// Nodes, strings, authors and evidence: the reads, and the mutations
// every change goes through. A mutation validates its input, encodes
// one op (oplog.go) and commits it: the log record is written first,
// then the same apply code replay uses changes memory.

package know

import (
  "encoding/binary"
  "errors"
  "fmt"
  "strings"
  "time"
)

// maxNodes is how many IDs fit beside an edge's 3 kind bits.
const maxNodes = 1 << 29

var (
  ErrReadOnly = errors.New(
    "the tree is open read-only (another armin holds it)",
  )
  ErrNoNode   = errors.New("no such node")
)

var epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// today is the day number stamped on Born and Seen; tests replace it.
var today = func() uint16 {
  return uint16(time.Since(epoch) / (24 * time.Hour))
}

// Day turns a Born or Seen day number back into a date.
func Day(d uint16) time.Time {
  return epoch.AddDate(0, 0, int(d))
}

func (n Node) Kind() Kind { return Kind(n.KindConf & 0b111) }
func (n Node) Conf() Conf { return Conf(n.KindConf >> 3 & 0b11) }

func kindConf(k Kind, c Conf) uint8 {
  return uint8(k) & 0b111 | uint8(c) & 0b11 << 3
}

// Len is the number of node IDs, tombstones included.
func (g *Graph) Len() int {
  return len(g.nodes) + len(g.moreNodes)
}

func (g *Graph) node(id NodeID) *Node {
  if int(id) < len(g.nodes) {
    return &g.nodes[id]
  }

  return &g.moreNodes[int(id) - len(g.nodes)]
}

func (g *Graph) valid(id NodeID) bool {
  return int(id) < g.Len()
}

func (g *Graph) live(id NodeID) bool {
  return g.valid(id) && g.node(id).Flags & FlagDeleted == 0
}

// Node returns a copy of node id's record.
func (g *Graph) Node(id NodeID) Node {
  return *g.node(id)
}

// Live reports whether id exists and isn't deleted.
func (g *Graph) Live(id NodeID) bool {
  return g.live(id)
}

// Bytes returns a string from the arena without copying; it aliases
// the snapshot, so it must not outlive Close.
func (g *Graph) Bytes(ref StrRef) []byte {
  buf := g.arena
  off := int(ref)
  if off >= len(g.arena) {
    buf = g.moreArena
    off -= len(g.arena)
  }

  n, w := binary.Uvarint(buf[off:])
  start := off + w

  return buf[start:start + int(n)]
}

// Str returns a copy of a string from the arena.
func (g *Graph) Str(ref StrRef) string {
  return string(g.Bytes(ref))
}

func (g *Graph) Name(id NodeID) string  { return g.Str(g.node(id).Name) }
func (g *Graph) Claim(id NodeID) string { return g.Str(g.node(id).Claim) }

// intern appends s to the heap arena and returns its reference.
func (g *Graph) intern(s []byte) StrRef {
  if len(s) == 0 {
    return 0
  }

  ref := StrRef(len(g.arena) + len(g.moreArena))
  g.moreArena = binary.AppendUvarint(g.moreArena, uint64(len(s)))
  g.moreArena = append(g.moreArena, s...)

  return ref
}

// Path is the node's names from the root, joined by "/"; the root's
// path is "".
func (g *Graph) Path(id NodeID) string {
  var parts [64]NodeID
  n := 0
  for id != 0 && n < len(parts) {
    parts[n] = id
    n++
    id = g.node(id).Parent
  }

  var b strings.Builder
  for i := n - 1; i >= 0; i-- {
    b.Write(g.Bytes(g.node(parts[i]).Name))
    if i > 0 {
      b.WriteByte('/')
    }
  }

  return b.String()
}

// Lookup finds a node by its path; "" is the root.
func (g *Graph) Lookup(path string) (NodeID, bool) {
  id := NodeID(0)
  for path != "" {
    name := path
    i := strings.IndexByte(path, '/')
    if i >= 0 {
      name = path[:i]
      path = path[i + 1:]
    } else {
      path = ""
    }

    next, ok := g.Child(id, name)
    if !ok {
      return 0, false
    }

    id = next
  }

  return id, true
}

// Authors is the number of interned authors.
func (g *Graph) Authors() int {
  return len(g.authors)
}

// Author returns author a's record.
func (g *Graph) Author(a AuthorID) Author {
  return g.authors[a]
}

// Ev returns evidence e's record.
func (g *Graph) Ev(e EvidenceID) Evidence {
  if int(e) < len(g.evidence) {
    return g.evidence[e]
  }

  return g.moreEv[int(e) - len(g.evidence)]
}

func (g *Graph) ev(e EvidenceID) *Evidence {
  if int(e) < len(g.evidence) {
    return &g.evidence[e]
  }

  return &g.moreEv[int(e) - len(g.evidence)]
}

// EvidenceOf appends the live evidence of node id to buf: a binary
// search over the snapshot's sorted part, then a scan of what was
// added since.
func (g *Graph) EvidenceOf(id NodeID, buf []EvidenceID) []EvidenceID {
  lo, hi := 0, len(g.evidence)
  for lo < hi {
    mid := int(uint(lo + hi) >> 1)
    if g.evidence[mid].Node < id {
      lo = mid + 1
    } else {
      hi = mid
    }
  }

  for i := lo; i < len(g.evidence) && g.evidence[i].Node == id; i++ {
    if g.evidence[i].Kind != refDeleted {
      buf = append(buf, EvidenceID(i))
    }
  }

  for i := range g.moreEv {
    e := &g.moreEv[i]
    if e.Node == id && e.Kind != refDeleted {
      buf = append(buf, EvidenceID(len(g.evidence) + i))
    }
  }

  return buf
}

// EvidenceCount is the number of evidence IDs, deleted ones included.
func (g *Graph) EvidenceCount() int {
  return len(g.evidence) + len(g.moreEv)
}

// validName rejects names that would break paths or the dump format.
func validName(name string) error {
  if name == "" {
    return errors.New("a node needs a name")
  }

  if len(name) > 200 {
    return errors.New("a node name is at most 200 bytes")
  }

  for i := 0; i < len(name); i++ {
    c := name[i]
    if c == '/' || c <= ' ' || c == '"' {
      return fmt.Errorf("a node name can't contain %q", c)
    }
  }

  return nil
}

// AddNode adds a child of parent, stamped born and seen today.
func (g *Graph) AddNode(
  parent NodeID, name, claim string, kind Kind, conf Conf, author AuthorID,
) (NodeID, error) {
  if !g.live(parent) {
    return 0, fmt.Errorf(
      "\n\t... in AddNode:\n\tparent %d: %w", parent, ErrNoNode,
    )
  }

  err := validName(name)
  if err != nil {
    return 0, fmt.Errorf("\n\t... in AddNode:\n\t%w", err)
  }

  _, taken := g.Child(parent, name)
  if taken {
    return 0, fmt.Errorf(
      "\n\t... in AddNode:\n\t%s/%s already exists", g.Path(parent), name,
    )
  }

  if g.Len() >= maxNodes {
    return 0, errors.New("\n\t... in AddNode:\n\tthe tree is full")
  }

  if int(author) >= len(g.authors) {
    return 0, fmt.Errorf("\n\t... in AddNode:\n\tno author %d", author)
  }

  id := NodeID(g.Len())
  d := today()
  g.begin(opNode)
  g.uv(uint64(id))
  g.uv(uint64(parent))
  g.uv(uint64(kindConf(kind, conf)))
  g.uv(0)
  g.uv(uint64(d))
  g.uv(uint64(d))
  g.uv(uint64(author))
  g.str(name)
  g.str(claim)

  return id, g.commit()
}

// SetClaim replaces a node's claim and marks it seen.
func (g *Graph) SetClaim(id NodeID, claim string) error {
  if !g.live(id) {
    return fmt.Errorf("\n\t... in SetClaim:\n\t%d: %w", id, ErrNoNode)
  }

  g.begin(opClaim)
  g.uv(uint64(id))
  g.str(claim)
  err := g.commit()
  if err != nil {
    return err
  }

  return g.touch(id)
}

// SetKind changes a node's kind and confidence.
func (g *Graph) SetKind(id NodeID, kind Kind, conf Conf) error {
  if !g.live(id) {
    return fmt.Errorf("\n\t... in SetKind:\n\t%d: %w", id, ErrNoNode)
  }

  n := g.node(id)

  return g.meta(id, kindConf(kind, conf), n.Flags, today())
}

// SetFlags sets the flags in set and clears those in clear.
func (g *Graph) SetFlags(id NodeID, set, clear NodeFlags) error {
  if !g.valid(id) {
    return fmt.Errorf("\n\t... in SetFlags:\n\t%d: %w", id, ErrNoNode)
  }

  n := g.node(id)
  if clear & FlagDeleted != 0 && n.Flags & FlagDeleted != 0 {
    return errors.New("\n\t... in SetFlags:\n\ta deleted node stays deleted")
  }

  flags := n.Flags &^ clear | set
  if flags == n.Flags {
    return nil
  }

  return g.meta(id, n.KindConf, flags, n.Seen)
}

func (g *Graph) touch(id NodeID) error {
  n := g.node(id)
  d := today()
  if n.Seen == d {
    return nil
  }

  return g.meta(id, n.KindConf, n.Flags, d)
}

// Touch marks a node seen today.
func (g *Graph) Touch(id NodeID) error {
  if !g.live(id) {
    return fmt.Errorf("\n\t... in Touch:\n\t%d: %w", id, ErrNoNode)
  }

  return g.touch(id)
}

func (g *Graph) meta(id NodeID, kc uint8, flags NodeFlags, seen uint16) error {
  g.begin(opMeta)
  g.uv(uint64(id))
  g.uv(uint64(kc))
  g.uv(uint64(flags))
  g.uv(uint64(seen))

  return g.commit()
}

// Delete marks a node deleted; its ID is never reused. Its children
// must be moved or deleted first.
func (g *Graph) Delete(id NodeID) error {
  if id == 0 || !g.live(id) {
    return fmt.Errorf("\n\t... in Delete:\n\t%d: %w", id, ErrNoNode)
  }

  var kids [1]NodeID
  if len(g.Children(id, kids[:0])) > 0 {
    return fmt.Errorf("\n\t... in Delete:\n\t%s still has children", g.Path(id))
  }

  return g.SetFlags(id, FlagDeleted, 0)
}

// Move gives a node a new parent and name.
func (g *Graph) Move(id, parent NodeID, name string) error {
  if id == 0 || !g.live(id) || !g.live(parent) {
    return fmt.Errorf("\n\t... in Move:\n\t%d → %d: %w", id, parent, ErrNoNode)
  }

  err := validName(name)
  if err != nil {
    return fmt.Errorf("\n\t... in Move:\n\t%w", err)
  }

  other, taken := g.Child(parent, name)
  if taken && other != id {
    return fmt.Errorf(
      "\n\t... in Move:\n\t%s/%s already exists", g.Path(parent), name,
    )
  }

  for p := parent; p != 0; p = g.node(p).Parent {
    if p == id {
      return errors.New("\n\t... in Move:\n\ta node can't move under itself")
    }
  }

  g.begin(opMove)
  g.uv(uint64(id))
  g.uv(uint64(parent))
  g.str(name)

  return g.commit()
}

// Intern returns the ID of an author, adding it the first time.
func (g *Graph) Intern(
  role Role, model, session string, delegation uint32,
) (AuthorID, error) {
  for i := range g.authors {
    a := &g.authors[i]
    if a.Role == role && a.Delegation == delegation &&
      string(g.Bytes(a.Model)) == model &&
      string(g.Bytes(a.Session)) == session {
      return AuthorID(i), nil
    }
  }

  if len(g.authors) >= 1 << 16 {
    return 0, errors.New("\n\t... in Intern:\n\tthe author table is full")
  }

  id := AuthorID(len(g.authors))
  g.begin(opAuthor)
  g.uv(uint64(role))
  g.uv(uint64(delegation))
  g.str(model)
  g.str(session)

  return id, g.commit()
}

// AddEvidence attaches evidence to a node.
func (g *Graph) AddEvidence(
  id NodeID, kind RefKind, ref string, hash uint32,
) (EvidenceID, error) {
  if !g.live(id) {
    return 0, fmt.Errorf("\n\t... in AddEvidence:\n\t%d: %w", id, ErrNoNode)
  }

  if kind > RefSymbol {
    return 0, fmt.Errorf("\n\t... in AddEvidence:\n\tbad kind %d", kind)
  }

  e := EvidenceID(g.EvidenceCount())
  g.begin(opEvidence)
  g.uv(uint64(id))
  g.uv(uint64(kind))
  g.uv(uint64(hash))
  g.str(ref)

  return e, g.commit()
}

// Reanchor points evidence at new content: code that moved, or a
// reference the model corrected.
func (g *Graph) Reanchor(e EvidenceID, ref string, hash uint32) error {
  if int(e) >= g.EvidenceCount() || g.ev(e).Kind == refDeleted {
    return fmt.Errorf("\n\t... in Reanchor:\n\tno evidence %d", e)
  }

  g.begin(opReanchor)
  g.uv(uint64(e))
  g.uv(uint64(hash))
  g.str(ref)

  return g.commit()
}

// RemoveEvidence deletes one piece of evidence.
func (g *Graph) RemoveEvidence(e EvidenceID) error {
  if int(e) >= g.EvidenceCount() || g.ev(e).Kind == refDeleted {
    return fmt.Errorf("\n\t... in RemoveEvidence:\n\tno evidence %d", e)
  }

  g.begin(opUnevidence)
  g.uv(uint64(e))

  return g.commit()
}

// AddEdge adds from → to of the given kind; adding one that exists
// is a no-op.
func (g *Graph) AddEdge(from, to NodeID, kind EdgeKind) error {
  if !g.live(from) || !g.live(to) {
    return fmt.Errorf("\n\t... in AddEdge:\n\t%d → %d: %w", from, to, ErrNoNode)
  }

  if from == to {
    return errors.New("\n\t... in AddEdge:\n\ta node can't point at itself")
  }

  if kind > EdgeDerivedFrom {
    return fmt.Errorf("\n\t... in AddEdge:\n\tbad kind %d", kind)
  }

  if g.HasEdge(from, to, kind) {
    return nil
  }

  g.begin(opEdge)
  g.uv(uint64(from))
  g.uv(uint64(pack(to, kind)))

  return g.commit()
}

// RemoveEdge removes from → to of the given kind, if there is one.
func (g *Graph) RemoveEdge(from, to NodeID, kind EdgeKind) error {
  if !g.HasEdge(from, to, kind) {
    return nil
  }

  g.begin(opUnedge)
  g.uv(uint64(from))
  g.uv(uint64(pack(to, kind)))

  return g.commit()
}
