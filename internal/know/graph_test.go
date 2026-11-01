package know

import (
  "bytes"
  "fmt"
  "testing"
)

func init() {
  today = func() uint16 { return 303 }
}

// dumpAll is a graph's full text form, used to compare states.
func dumpAll(t *testing.T, g *Graph) string {
  t.Helper()
  var b bytes.Buffer
  err := g.Dump(&b, 0)
  if err != nil {
    t.Fatal(err)
  }

  return b.String()
}

// need unwraps a (value, error) pair; a test calling it fails with
// the panic's stack when err is set.
func need[T any](v T, err error) T {
  if err != nil {
    panic(err)
  }

  return v
}

func mustDo(t *testing.T, err error) {
  t.Helper()
  if err != nil {
    t.Fatal(err)
  }
}

// sample builds a small tree: tcp/{handshake, seqnum, window} with
// requires edges, a minion author and evidence.
func sample(t *testing.T, g *Graph) {
  t.Helper()
  m := need(g.Intern(RoleMinion, "qwen3.5-9b", "s1", 7))
  tcp := need(g.AddNode(0, "tcp", "", KindTopic, ConfChecked, 0))
  hs := need(
    g.AddNode(tcp, "handshake", "SYN, SYN-ACK, ACK", KindFact, ConfChecked, 0),
  )
  seq := need(
    g.AddNode(
      tcp, "seqnum", "bytes are numbered", KindConcept, ConfInferred, m,
    ),
  )
  win := need(
    g.AddNode(tcp, "window", "flow control", KindConcept, ConfGuess, m),
  )
  mustDo(t, g.AddEdge(hs, seq, EdgeRequires))
  mustDo(t, g.AddEdge(win, seq, EdgeRequires))
  mustDo(t, g.AddEdge(win, hs, EdgeDerivedFrom))
  need(g.AddEvidence(hs, RefFile, "tcp/conn.go:10-20", 0xdeadbeef))
  need(g.AddEvidence(seq, RefURL, "https://www.rfc-editor.org/rfc/rfc9293", 1))
}

func TestNodesAndPaths(t *testing.T) {
  g := New()
  sample(t, g)

  hs, ok := g.Lookup("tcp/handshake")
  if !ok || g.Path(hs) != "tcp/handshake" ||
      g.Claim(hs) != "SYN, SYN-ACK, ACK" {
    t.Fatalf("lookup: %d %v %q", hs, ok, g.Path(hs))
  }

  n := g.Node(hs)
  if n.Kind() != KindFact || n.Conf() != ConfChecked || n.Born != 303 {
    t.Fatalf("node: %+v", n)
  }

  _, err := g.AddNode(
    g.Node(hs).Parent, "handshake", "", KindFact, ConfGuess, 0,
  )
  if err == nil {
    t.Fatal("a duplicate sibling name should be refused")
  }

  for _, bad := range []string{ "", "a/b", "a b", "q\"" } {
    _, err = g.AddNode(0, bad, "", KindFact, ConfGuess, 0)
    if err == nil {
      t.Errorf("name %q should be refused", bad)
    }
  }

  tcp, _ := g.Lookup("tcp")
  kids := g.Children(tcp, nil)
  if len(kids) != 3 || g.Name(kids[0]) != "handshake" {
    t.Fatalf("children: %v", kids)
  }

  udp := need(g.AddNode(0, "udp", "", KindTopic, ConfGuess, 0))
  mustDo(t, g.Move(hs, udp, "hs"))
  _, ok = g.Lookup("tcp/handshake")
  if ok || g.Path(hs) != "udp/hs" {
    t.Fatalf("after move: %q", g.Path(hs))
  }

  if g.Move(udp, hs, "x") == nil {
    t.Fatal("moving a node under its own child should fail")
  }

  if g.Delete(udp) == nil {
    t.Fatal("deleting a node with children should fail")
  }

  mustDo(t, g.Delete(hs))
  _, ok = g.Lookup("udp/hs")
  if ok || g.Live(hs) || len(g.Children(udp, nil)) != 0 {
    t.Fatal("a deleted node should be gone from paths and children")
  }

  if g.SetFlags(hs, 0, FlagDeleted) == nil {
    t.Fatal("undeleting should be refused")
  }

  // the name is free again
  need(g.AddNode(udp, "hs", "", KindFact, ConfGuess, 0))
}

func TestEdges(t *testing.T) {
  g := New()
  sample(t, g)
  hs, _ := g.Lookup("tcp/handshake")
  seq, _ := g.Lookup("tcp/seqnum")
  win, _ := g.Lookup("tcp/window")

  in := g.In(seq, nil)
  if len(in) != 2 {
    t.Fatalf("in(seq) = %v", in)
  }

  out := g.Out(win, nil)
  if len(out) != 2 || !g.HasEdge(win, hs, EdgeDerivedFrom) {
    t.Fatalf("out(win) = %v", out)
  }

  mustDo(t, g.AddEdge(win, seq, EdgeRequires)) // duplicate: no-op
  if len(g.Out(win, nil)) != 2 {
    t.Fatal("a duplicate edge was added")
  }

  if g.AddEdge(win, win, EdgeRequires) == nil {
    t.Fatal("a self edge should be refused")
  }

  mustDo(t, g.RemoveEdge(win, seq, EdgeRequires))
  if g.HasEdge(win, seq, EdgeRequires) || len(g.In(seq, nil)) != 1 {
    t.Fatal("edge not removed")
  }
}

// TestFold pushes the delta past foldAt and checks edges survive the
// fold, then removes edges that now live in CSR.
func TestFold(t *testing.T) {
  g := New()
  ids := make([]NodeID, 60)
  for i := range ids {
    ids[i] = need(
      g.AddNode(0, fmt.Sprintf("c%d", i), "", KindConcept, ConfGuess, 0),
    )
  }

  n := 0
  for i := range ids {
    for j := range ids {
      if i != j && (i * 7 + j) % 3 == 0 {
        mustDo(t, g.AddEdge(ids[i], ids[j], EdgeRequires))
        n++
      }
    }
  }

  if n <= foldAt || len(g.edges.delta) >= foldAt {
    t.Fatalf("expected a fold: %d edges, delta %d", n, len(g.edges.delta))
  }

  if g.edges.count() != n {
    t.Fatalf("count = %d, want %d", g.edges.count(), n)
  }

  for i := range ids {
    for j := range ids {
      want := i != j && (i * 7 + j) % 3 == 0
      if g.HasEdge(ids[i], ids[j], EdgeRequires) != want {
        t.Fatalf("edge %d → %d: want %v", i, j, want)
      }
    }
  }

  // a CSR edge removed becomes a tombstone in both directions
  a, b := ids[0], ids[3]
  mustDo(t, g.RemoveEdge(a, b, EdgeRequires))
  for _, e := range g.In(b, nil) {
    if e.To == a {
      t.Fatal("removed edge still in the in arrays")
    }
  }

  if g.edges.count() != n - 1 {
    t.Fatalf("count after removal = %d", g.edges.count())
  }
}

func TestEvidence(t *testing.T) {
  g := New()
  sample(t, g)
  hs, _ := g.Lookup("tcp/handshake")

  evs := g.EvidenceOf(hs, nil)
  if len(evs) != 1 || g.Str(g.Ev(evs[0]).Ref) != "tcp/conn.go:10-20" {
    t.Fatalf("evidence: %v", evs)
  }

  mustDo(t, g.Reanchor(evs[0], "tcp/conn.go:30-40", 42))
  e := g.Ev(evs[0])
  if g.Str(e.Ref) != "tcp/conn.go:30-40" || e.Hash != 42 {
    t.Fatalf("reanchored: %+v", e)
  }

  mustDo(t, g.RemoveEvidence(evs[0]))
  if len(g.EvidenceOf(hs, nil)) != 0 {
    t.Fatal("evidence not removed")
  }
}

func TestIntern(t *testing.T) {
  g := New()
  a := need(g.Intern(RoleCopilot, "glm", "s", 0))
  b := need(g.Intern(RoleCopilot, "glm", "s", 0))
  c := need(g.Intern(RoleCopilot, "glm", "s", 1))
  if a != b || a == c || g.Authors() != 3 {
    t.Fatalf("intern: %d %d %d, %d authors", a, b, c, g.Authors())
  }
}

func TestReadAllocations(t *testing.T) {
  g := New()
  sample(t, g)
  hs, _ := g.Lookup("tcp/handshake")
  buf := make([]Edge, 0, 8)
  allocs := testing.AllocsPerRun(
    100,
    func() {
      g.Out(hs, buf[:0])
      g.In(hs, buf[:0])
      g.Child(0, "tcp")
      g.Bytes(g.Node(hs).Claim)
    },
  )

  if allocs != 0 {
    t.Fatalf("reads allocate: %v per run", allocs)
  }
}
