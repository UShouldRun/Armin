// What models read: one line per node, and the outline frozen into
// the system prompt.
//
//   n42 tcp/handshake fact checked minion-unverified needs n17 n23 used-by n8 "three segments: SYN, SYN-ACK, ACK; the server…" STALE
//
// Plain words, no legend needed; unset fields are left out; the claim
// is cut at ClaimCut characters (know_get returns it whole).

package know

import (
  "fmt"
  "strconv"
  "strings"
)

// ClaimCut is where a claim is cut in a node line.
const ClaimCut = 100

// Line appends node id's line to b.
func (g *Graph) Line(b *strings.Builder, id NodeID, full bool) {
  n := g.node(id)
  fmt.Fprintf(b, "n%d %s %s %s", id, g.Path(id), n.Kind(), n.Conf())
  if g.authors[n.Author].Role == RoleMinion && n.Flags & FlagVerified == 0 {
    b.WriteString(" minion-unverified")
  }

  if n.Flags & FlagChosen != 0 {
    b.WriteString(" chosen")
  }

  var buf [32]Edge
  out := g.Out(id, buf[:0])
  g.edgeWords(b, out, "needs", func(k EdgeKind) bool {
    return k == EdgeRequires || k == EdgeDerivedFrom
  })

  for _, k := range []EdgeKind{ EdgeImplements, EdgeExplains, EdgeContradicts } {
    g.edgeWords(b, out, k.String(), func(e EdgeKind) bool { return e == k })
  }

  in := g.In(id, buf[:0])
  g.edgeWords(b, in, "used-by", func(k EdgeKind) bool {
    return k == EdgeRequires || k == EdgeDerivedFrom
  })

  claim := string(g.Bytes(n.Claim))
  if claim != "" {
    if !full {
      claim = cut(claim, ClaimCut)
    }

    b.WriteByte(' ')
    b.WriteString(strconv.Quote(claim))
  }

  for _, f := range []struct {
    flag NodeFlags
    word string
  }{
    { FlagStale, " STALE" },
    { FlagSuspect, " SUSPECT" },
    { FlagCycle, " CYCLE" },
  } {
    if n.Flags & f.flag != 0 {
      b.WriteString(f.word)
    }
  }
}

// edgeWords writes " word nA nB" for the live edges keep accepts.
func (g *Graph) edgeWords(
  b *strings.Builder, edges []Edge, word string, keep func(EdgeKind) bool,
) {
  first := true
  for _, e := range edges {
    if !keep(e.Kind) || !g.live(e.To) {
      continue
    }

    if first {
      b.WriteByte(' ')
      b.WriteString(word)
      first = false
    }

    fmt.Fprintf(b, " n%d", e.To)
  }
}

// cut shortens s to at most n runes, ending with "…" when cut, at a
// space when there is one near the end.
func cut(s string, n int) string {
  if len(s) <= n {
    return s
  }

  r := []rune(s)
  if len(r) <= n {
    return s
  }

  r = r[:n]
  i := len(r) - 1
  for i > n * 3 / 4 && r[i] != ' ' {
    i--
  }

  if r[i] == ' ' {
    r = r[:i]
  }

  return string(r) + "…"
}

// Outline is the top two levels of the tree with how many nodes each
// branch holds, one top-level node per line, capped at max bytes:
//
//   tcp (14): handshake 3, seqnum, window 6
//
// Pinned nodes come first; past the cap, a last line says how many
// branches were left out.
func (g *Graph) Outline(max int) string {
  top := g.Children(0, nil)
  if len(top) == 0 {
    return ""
  }

  size := g.subtreeSizes()
  pinned := func(id NodeID) int {
    if g.node(id).Flags & FlagPinned != 0 {
      return 0
    }

    return 1
  }

  // pinned first, then the biggest branches
  for i := 1; i < len(top); i++ {
    for j := i; j > 0; j-- {
      a, b := top[j - 1], top[j]
      if pinned(a) < pinned(b) || pinned(a) == pinned(b) && size[a] >= size[b] {
        break
      }

      top[j - 1], top[j] = b, a
    }
  }

  var out strings.Builder
  var line strings.Builder
  var kids []NodeID
  for i, t := range top {
    line.Reset()
    fmt.Fprintf(&line, "%s (%d)", g.Name(t), size[t] - 1)
    kids = g.Children(t, kids[:0])
    for k, c := range kids {
      if k == 0 {
        line.WriteString(": ")
      } else {
        line.WriteString(", ")
      }

      line.WriteString(g.Name(c))
      if size[c] > 1 {
        fmt.Fprintf(&line, " %d", size[c] - 1)
      }
    }

    line.WriteByte('\n')
    room := max
    if i < len(top) - 1 {
      room -= len("… 99999 more\n") // keep space for the rest count
    }

    if out.Len() + line.Len() > room {
      if out.Len() == 0 {
        return cut(line.String(), max - 1) + "\n"
      }

      fmt.Fprintf(&out, "… %d more\n", len(top) - i)
      break
    }

    out.WriteString(line.String())
  }

  return out.String()
}

// subtreeSizes counts each live node's subtree, itself included, in
// one pass over the preorder (children follow their parent).
func (g *Graph) subtreeSizes() []int32 {
  size := make([]int32, g.Len())
  order := g.preorder(0, nil)
  for i := len(order) - 1; i >= 0; i-- {
    id := order[i]
    size[id]++
    p := g.node(id).Parent
    size[p] += size[id]
  }

  return size
}
