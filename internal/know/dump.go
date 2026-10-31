// The text form of a subtree, for armin -know dump and for editing by
// hand. One block per node, depth first:
//
//   n42 tcp/handshake fact checked a3 born 2026-10-31 seen 2026-11-02
//     claim "three segments: SYN, SYN-ACK, ACK"
//     flags stale verified
//     requires n17 n23
//     evidence file "internal/tcp/conn.go:120-160" 9f3a01c2
//
// Authors used by the subtree are listed first as "author a3 minion
// model "…" session "…" delegation 7". Unset lines are left out.

package know

import (
  "bufio"
  "fmt"
  "io"
  "strconv"
  "strings"
)

// Dump writes the subtree under id (id included, unless it is the
// root) in the text form.
func (g *Graph) Dump(w io.Writer, id NodeID) error {
  if !g.live(id) {
    return fmt.Errorf("\n\t... in Dump:\n\t%d: %w", id, ErrNoNode)
  }

  b := bufio.NewWriter(w)
  b.WriteString("# armin know v1\n")
  b.WriteString("# edit claims, kinds, flags, paths and edges; add nodes with\n")
  b.WriteString("# \"new <path> <kind> <conf>\" blocks; evidence lines are read-only\n")

  order := g.preorder(id, nil)
  used := make([]bool, len(g.authors))
  for _, n := range order {
    used[g.node(n).Author] = true
  }

  for i, u := range used {
    if !u {
      continue
    }

    a := g.authors[i]
    fmt.Fprintf(b, "author a%d %s", i, a.Role)
    model, session := g.Str(a.Model), g.Str(a.Session)
    if model != "" {
      fmt.Fprintf(b, " model %s", strconv.Quote(model))
    }

    if session != "" {
      fmt.Fprintf(b, " session %s", strconv.Quote(session))
    }

    if a.Delegation != 0 {
      fmt.Fprintf(b, " delegation %d", a.Delegation)
    }

    b.WriteByte('\n')
  }

  var edges []Edge
  var evs []EvidenceID
  var words []string
  for _, n := range order {
    nd := g.node(n)
    fmt.Fprintf(
      b, "\nn%d %s %s %s a%d born %s seen %s\n",
      n, g.Path(n), nd.Kind(), nd.Conf(), nd.Author,
      Day(nd.Born).Format("2006-01-02"), Day(nd.Seen).Format("2006-01-02"),
    )

    claim := g.Bytes(nd.Claim)
    if len(claim) > 0 {
      fmt.Fprintf(b, "  claim %s\n", strconv.Quote(string(claim)))
    }

    words = FlagWords(nd.Flags, words[:0])
    if len(words) > 0 {
      fmt.Fprintf(b, "  flags %s\n", strings.Join(words, " "))
    }

    edges = g.Out(n, edges[:0])
    for k := EdgeRequires; k <= EdgeDerivedFrom; k++ {
      line := false
      for _, e := range edges {
        if e.Kind != k || !g.live(e.To) {
          continue
        }

        if !line {
          fmt.Fprintf(b, "  %s", k)
          line = true
        }

        fmt.Fprintf(b, " n%d", e.To)
      }

      if line {
        b.WriteByte('\n')
      }
    }

    evs = g.EvidenceOf(n, evs[:0])
    for _, e := range evs {
      ev := g.Ev(e)
      fmt.Fprintf(
        b, "  evidence %s %s %08x\n",
        ev.Kind, strconv.Quote(g.Str(ev.Ref)), ev.Hash,
      )
    }
  }

  return b.Flush()
}

// preorder appends the live subtree under id, depth first, children
// in ID order; the root itself is left out.
func (g *Graph) preorder(id NodeID, buf []NodeID) []NodeID {
  start := len(buf)
  if id != 0 {
    buf = append(buf, id)
  } else {
    buf = g.Children(0, buf)
    for i, j := start, len(buf) - 1; i < j; i, j = i + 1, j - 1 {
      buf[i], buf[j] = buf[j], buf[i]
    }
  }

  // an explicit stack, reversed children, so it pops in ID order
  stack := append([]NodeID(nil), buf[start:]...)
  buf = buf[:start]
  var kids []NodeID
  for len(stack) > 0 {
    n := stack[len(stack) - 1]
    stack = stack[:len(stack) - 1]
    buf = append(buf, n)
    kids = g.Children(n, kids[:0])
    for i := len(kids) - 1; i >= 0; i-- {
      stack = append(stack, kids[i])
    }
  }

  return buf
}
