// The append-only log. A file starts with "ARML" and the generation
// of the snapshot it extends; then come records of
//
//   [op u8][payload length uvarint][payload][crc32 of everything before]
//
// Mutations build a record with begin/uv/str, and commit writes it
// before applying it; replay applies the same records at load, and
// stops at the first torn or corrupt one.

package know

import (
  "bufio"
  "encoding/binary"
  "errors"
  "fmt"
  "hash/crc32"
  "io"
  "os"
)

const logMagic = "ARML"

// maxPayload rejects absurd lengths in a corrupt log before any
// allocation.
const maxPayload = 1 << 20

var errCorrupt = errors.New("corrupt record")

func (g *Graph) begin(op opCode) {
  g.op = append(g.op[:0], byte(op))
}

func (g *Graph) uv(v uint64) {
  g.op = binary.AppendUvarint(g.op, v)
}

func (g *Graph) str(s string) {
  g.op = binary.AppendUvarint(g.op, uint64(len(s)))
  g.op = append(g.op, s...)
}

// commit writes the op in g.op to the log, then applies it.
func (g *Graph) commit() error {
  if g.ro {
    return ErrReadOnly
  }

  if g.log != nil {
    rec := frame(nil, g.op)
    _, err := g.log.Write(rec)
    if err != nil {
      return fmt.Errorf("\n\t... in commit:\n\t%w", err)
    }
  }

  return g.apply(opCode(g.op[0]), g.op[1:])
}

// frame appends the record for body (op byte + payload) to dst.
func frame(dst, body []byte) []byte {
  start := len(dst)
  dst = append(dst, body[0])
  dst = binary.AppendUvarint(dst, uint64(len(body) - 1))
  dst = append(dst, body[1:]...)

  return binary.LittleEndian.AppendUint32(dst, crc32.ChecksumIEEE(dst[start:]))
}

func (d *decoder) uv() uint64 {
  if d.err != nil {
    return 0
  }

  v, n := binary.Uvarint(d.p)
  if n <= 0 {
    d.err = errCorrupt
    return 0
  }

  d.p = d.p[n:]

  return v
}

// u32 reads a uvarint that must fit below limit.
func (d *decoder) u32(limit uint64) uint32 {
  v := d.uv()
  if v >= limit {
    d.err = errCorrupt
    return 0
  }

  return uint32(v)
}

func (d *decoder) bytes() []byte {
  n := d.uv()
  if d.err != nil {
    return nil
  }

  if n > uint64(len(d.p)) {
    d.err = errCorrupt
    return nil
  }

  s := d.p[:n]
  d.p = d.p[n:]

  return s
}

// apply changes memory for one op. It checks what a corrupt log could
// get wrong; mutations have already validated their input.
func (g *Graph) apply(op opCode, payload []byte) error {
  d := decoder{ p: payload }
  switch op {
  case opNode:
    id := d.u32(maxNodes)
    parent := d.u32(maxNodes)
    n := Node{ Parent: NodeID(parent) }
    n.KindConf = uint8(d.u32(1 << 5))
    n.Flags = NodeFlags(d.u32(1 << 8))
    n.Born = uint16(d.u32(1 << 16))
    n.Seen = uint16(d.u32(1 << 16))
    n.Author = AuthorID(d.u32(uint64(len(g.authors))))
    name := d.bytes()
    claim := d.bytes()
    if d.err != nil || int(id) != g.Len() || (id != 0 && parent >= id) {
      return errCorrupt
    }

    n.Name = g.intern(name)
    n.Claim = g.intern(claim)
    g.moreNodes = append(g.moreNodes, n)
    if id != 0 && n.Flags & FlagDeleted == 0 {
      g.pathInsert(NodeID(id))
    }

  case opClaim:
    id := d.u32(uint64(g.Len()))
    claim := d.bytes()
    if d.err != nil {
      return errCorrupt
    }

    g.node(NodeID(id)).Claim = g.intern(claim)
    g.index.changed(NodeID(id))

  case opMeta:
    id := NodeID(d.u32(uint64(g.Len())))
    kc := uint8(d.u32(1 << 5))
    flags := NodeFlags(d.u32(1 << 8))
    seen := uint16(d.u32(1 << 16))
    if d.err != nil {
      return errCorrupt
    }

    n := g.node(id)
    was := n.Flags & FlagDeleted
    if id != 0 && was == 0 && flags & FlagDeleted != 0 {
      g.pathRemove(id)
    }

    n.KindConf, n.Flags, n.Seen = kc, flags, seen
    if was != flags & FlagDeleted {
      g.index.changed(id)
      g.index.eulerDirty = true
    }
    if id != 0 && was != 0 && flags & FlagDeleted == 0 {
      g.pathInsert(id)
    }

  case opMove:
    id := NodeID(d.u32(uint64(g.Len())))
    parent := NodeID(d.u32(uint64(g.Len())))
    name := d.bytes()
    if d.err != nil || id == 0 {
      return errCorrupt
    }

    n := g.node(id)
    live := n.Flags & FlagDeleted == 0
    if live {
      g.pathRemove(id)
    }

    n.Parent = parent
    n.Name = g.intern(name)
    g.index.changed(id)
    g.index.eulerDirty = true
    if live {
      g.pathInsert(id)
    }

  case opEdge, opUnedge:
    from := d.u32(uint64(g.Len()))
    tgt := d.u32(1 << 32)
    if d.err != nil || int(unpack(tgt).To) >= g.Len() {
      return errCorrupt
    }

    if op == opEdge {
      g.edges.add(from, tgt, g.Len())
    } else {
      g.edges.remove(from, tgt)
    }

  case opEvidence:
    e := Evidence{ Node: NodeID(d.u32(uint64(g.Len()))) }
    e.Kind = RefKind(d.u32(uint64(RefSymbol) + 1))
    e.Hash = d.u32(1 << 32)
    ref := d.bytes()
    if d.err != nil {
      return errCorrupt
    }

    e.Ref = g.intern(ref)
    g.moreEv = append(g.moreEv, e)

  case opReanchor:
    id := EvidenceID(d.u32(uint64(g.EvidenceCount())))
    hash := d.u32(1 << 32)
    ref := d.bytes()
    if d.err != nil {
      return errCorrupt
    }

    e := g.ev(id)
    e.Hash = hash
    e.Ref = g.intern(ref)

  case opUnevidence:
    id := EvidenceID(d.u32(uint64(g.EvidenceCount())))
    if d.err != nil {
      return errCorrupt
    }

    g.ev(id).Kind = refDeleted

  case opAuthor:
    a := Author{ Role: Role(d.u32(uint64(RoleAdvisor) + 1)) }
    a.Delegation = d.u32(1 << 32)
    model := d.bytes()
    session := d.bytes()
    if d.err != nil || len(g.authors) >= 1 << 16 {
      return errCorrupt
    }

    a.Model = g.intern(model)
    a.Session = g.intern(session)
    g.authors = append(g.authors, a)

  default:
    return errCorrupt
  }

  if len(d.p) != 0 {
    return errCorrupt
  }

  return nil
}

// replay applies the records of a log open at its start and returns
// the offset just past the last good one (0 when the log belongs to
// another generation) and the log's generation. A short read or a bad CRC
// ends it there: the rest is a torn write, cut off by the caller.
func (g *Graph) replay(f *os.File) (int64, uint64, error) {
  r := bufio.NewReaderSize(f, 64 * 1024)
  var head [12]byte
  _, err := io.ReadFull(r, head[:])
  if err != nil {
    return 0, 0, nil
  }

  if string(head[:4]) != logMagic {
    return 0, 0, errors.New("\n\t... in replay:\n\tnot an armin log")
  }

  // a log from another generation holds nothing to apply: an older
  // one is already in the snapshot
  gen := binary.LittleEndian.Uint64(head[4:])
  if gen != g.logGen {
    return 0, gen, nil
  }

  good := int64(len(head))
  var rec []byte
  for {
    op, err := r.ReadByte()
    if err != nil {
      return good, gen, nil
    }

    n, err := binary.ReadUvarint(r)
    if err != nil || n > maxPayload {
      return good, gen, nil
    }

    rec = append(rec[:0], op)
    rec = binary.AppendUvarint(rec, n)
    lenEnd := len(rec)
    rec = append(rec, make([]byte, n + 4)...)
    _, err = io.ReadFull(r, rec[lenEnd:])
    if err != nil {
      return good, gen, nil
    }

    end := len(rec) - 4
    sum := binary.LittleEndian.Uint32(rec[end:])
    if crc32.ChecksumIEEE(rec[:end]) != sum {
      return good, gen, nil
    }

    err = g.apply(opCode(op), rec[lenEnd:end])
    if err != nil {
      return good, gen, fmt.Errorf(
        "\n\t... in replay:\n\trecord at %d: %w", good, err,
      )
    }

    good += int64(len(rec))
  }
}
