// The snapshot: a 96-byte header, then the node, edge, evidence and
// author arrays and the string arena, each exactly as in memory. Load
// mmaps the file (private and copy-on-write) and points the base
// slices at it, so opening a tree reads only the pages it touches.
// Compaction writes a fresh snapshot without tombstones, garbage
// strings or the delta, renames it into place and starts a new log.

package know

import (
  "bytes"
  "encoding/binary"
  "errors"
  "fmt"
  "hash/crc32"
  "os"
  "path/filepath"
  "slices"
  "unsafe"

  "golang.org/x/sys/unix"
)

const (
  snapMagic   = "ARMK"
  snapVersion = 1
  headerSize  = 96
)

func init() {
  if unsafe.Sizeof(Node{}) != 20 || unsafe.Sizeof(Evidence{}) != 16 ||
    unsafe.Sizeof(Author{}) != 16 || unsafe.Sizeof(snapHeader{}) != headerSize {
    panic("know: record sizes changed; the snapshot format depends on them")
  }
}

// littleEndian reports whether the arrays can be used in place.
func littleEndian() bool {
  x := uint16(1)

  return *(*byte)(unsafe.Pointer(&x)) == 1
}

func asBytes[T any](s []T) []byte {
  if len(s) == 0 {
    return nil
  }

  size := int(unsafe.Sizeof(s[0]))

  return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s) * size)
}

func fromBytes[T any](b []byte, n int) []T {
  if n == 0 {
    return nil
  }

  return unsafe.Slice((*T)(unsafe.Pointer(&b[0])), n)
}

// loadSnap mmaps path and points g's base slices into it.
func (g *Graph) loadSnap(path string) error {
  f, err := os.Open(path)
  if err != nil {
    return err
  }

  defer f.Close()

  st, err := f.Stat()
  if err != nil {
    return fmt.Errorf("\n\t... in loadSnap:\n\t%w", err)
  }

  size := st.Size()
  if size < headerSize || size > 1 << 32 {
    return fmt.Errorf("\n\t... in loadSnap:\n\t%s: bad size %d", path, size)
  }

  m, err := unix.Mmap(
    int(f.Fd()), 0, int(size),
    unix.PROT_READ | unix.PROT_WRITE, unix.MAP_PRIVATE,
  )
  if err != nil {
    return fmt.Errorf("\n\t... in loadSnap:\n\t%w", err)
  }

  h, err := checkSnap(m)
  if err != nil {
    unix.Munmap(m)
    return fmt.Errorf("\n\t... in loadSnap:\n\t%s: %w", path, err)
  }

  g.mapped = m
  g.logGen = h.LogGen
  g.nodes = fromBytes[Node](m[h.OffNodes:], int(h.Nodes))
  g.edges.offs = fromBytes[uint32](m[h.OffOffs:], int(h.Nodes) + 1)
  g.edges.tgts = fromBytes[uint32](m[h.OffTgts:], int(h.Edges))
  g.evidence = fromBytes[Evidence](m[h.OffEvidence:], int(h.Evidence))
  g.authors = fromBytes[Author](m[h.OffAuthors:], int(h.Authors))
  g.arena = m[h.OffArena:h.OffArena + uint64(h.ArenaLen)]

  return nil
}

// checkSnap validates the header, the section bounds and the CRC.
func checkSnap(m []byte) (snapHeader, error) {
  var h snapHeader
  err := binary.Read(bytes.NewReader(m[:headerSize]), binary.LittleEndian, &h)
  if err != nil {
    return h, err
  }

  if string(h.Magic[:]) != snapMagic {
    return h, errors.New("not an armin tree snapshot")
  }

  if h.Version != snapVersion {
    return h, fmt.Errorf("snapshot version %d, want %d", h.Version, snapVersion)
  }

  if h.Size != uint64(len(m)) {
    return h, errors.New("truncated")
  }

  if crc32.ChecksumIEEE(m[headerSize:]) != h.CRC {
    return h, errors.New("checksum mismatch")
  }

  if h.Nodes == 0 || h.Nodes > maxNodes || h.Authors == 0 ||
      h.Authors > 1 << 16 {
    return h, errors.New("bad counts")
  }

  sections := []struct{ off, n uint64 }{
    { h.OffNodes, uint64(h.Nodes) * 20 },
    { h.OffOffs, (uint64(h.Nodes) + 1) * 4 },
    { h.OffTgts, uint64(h.Edges) * 4 },
    { h.OffEvidence, uint64(h.Evidence) * 16 },
    { h.OffAuthors, uint64(h.Authors) * 16 },
    { h.OffArena, uint64(h.ArenaLen) },
  }
  for _, s := range sections {
    if s.off % 8 != 0 || s.off < headerSize || s.off + s.n > h.Size {
      return h, errors.New("bad section bounds")
    }
  }

  if h.ArenaLen == 0 || m[h.OffArena] != 0 {
    return h, errors.New("bad arena")
  }

  offs := fromBytes[uint32](m[h.OffOffs:], int(h.Nodes) + 1)
  for i := 1; i < len(offs); i++ {
    if offs[i] < offs[i - 1] {
      return h, errors.New("bad edge offsets")
    }
  }

  if offs[h.Nodes] != h.Edges {
    return h, errors.New("bad edge offsets")
  }

  return h, nil
}

// encodeSnap builds a snapshot of g's current state, with only live
// strings in the arena, the delta folded in, tombstoned edges and
// deleted evidence dropped, and evidence sorted by node.
func (g *Graph) encodeSnap(gen uint64) []byte {
  n := g.Len()
  arena := []byte{ 0 }
  restr := func(ref StrRef) StrRef {
    s := g.Bytes(ref)
    if len(s) == 0 {
      return 0
    }

    out := StrRef(len(arena))
    arena = binary.AppendUvarint(arena, uint64(len(s)))
    arena = append(arena, s...)

    return out
  }

  nodes := make([]Node, n)
  for id := range nodes {
    nd := *g.node(NodeID(id))
    nd.Name = restr(nd.Name)
    nd.Claim = restr(nd.Claim)
    nodes[id] = nd
  }

  authors := make([]Author, len(g.authors))
  for i, a := range g.authors {
    a.Model = restr(a.Model)
    a.Session = restr(a.Session)
    authors[i] = a
  }

  ev := make([]Evidence, 0, g.EvidenceCount())
  for i := 0; i < g.EvidenceCount(); i++ {
    e := g.Ev(EvidenceID(i))
    if e.Kind == refDeleted || g.node(e.Node).Flags & FlagDeleted != 0 {
      continue
    }

    e.Ref = restr(e.Ref)
    ev = append(ev, e)
  }

  slices.SortStableFunc(ev, func(a, b Evidence) int {
    return int(a.Node) - int(b.Node)
  })

  g.edges.fold(n)

  h := snapHeader{
    Version:  snapVersion,
    LogGen:   gen,
    Nodes:    uint32(n),
    Edges:    uint32(len(g.edges.tgts)),
    Evidence: uint32(len(ev)),
    Authors:  uint32(len(authors)),
    ArenaLen: uint32(len(arena)),
  }
  copy(h.Magic[:], snapMagic)

  buf := make([]byte, headerSize)
  section := func(b []byte) uint64 {
    for len(buf) % 8 != 0 {
      buf = append(buf, 0)
    }

    off := uint64(len(buf))
    buf = append(buf, b...)

    return off
  }

  h.OffNodes = section(asBytes(nodes))
  h.OffOffs = section(asBytes(g.edges.offs))
  h.OffTgts = section(asBytes(g.edges.tgts))
  h.OffEvidence = section(asBytes(ev))
  h.OffAuthors = section(asBytes(authors))
  h.OffArena = section(arena)
  h.Size = uint64(len(buf))
  h.CRC = crc32.ChecksumIEEE(buf[headerSize:])

  var head bytes.Buffer
  binary.Write(&head, binary.LittleEndian, &h)
  copy(buf, head.Bytes())

  return buf
}

// writeFile writes data to path through a temporary file and a
// rename, syncing both, so a crash leaves the old file or the new.
func writeFile(path string, data []byte) error {
  tmp := path + ".tmp"
  f, err := os.OpenFile(tmp, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, 0o600)
  if err != nil {
    return err
  }

  _, err = f.Write(data)
  if err == nil {
    err = f.Sync()
  }

  closeErr := f.Close()
  if err == nil {
    err = closeErr
  }

  if err != nil {
    os.Remove(tmp)
    return err
  }

  err = os.Rename(tmp, path)
  if err != nil {
    return err
  }

  d, err := os.Open(filepath.Dir(path))
  if err != nil {
    return err
  }

  defer d.Close()

  return d.Sync()
}
