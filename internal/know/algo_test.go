package know

import (
  "fmt"
  "slices"
  "testing"
)

func ids(hs []Hit) []NodeID {
  out := make([]NodeID, len(hs))
  for i, h := range hs {
    out[i] = h.ID
  }

  return out
}

func TestFindRanksAndFilters(t *testing.T) {
  g := New()
  sample(t, g)
  udp := need(g.AddNode(0, "udp", "", KindTopic, ConfGuess, 0))
  mention := need(
    g.AddNode(
      udp,
      "nohandshake",
      "udp skips the handshake entirely",
      KindFact,
      ConfGuess,
      0,
    ),
  )
  hs, _ := g.Lookup("tcp/handshake")
  tcp, _ := g.Lookup("tcp")

  got := g.Find("handshake", 0, 5)
  if len(got) < 2 || got[0].ID != hs {
    t.Fatalf("handshake: %v; the node named handshake should rank first", got)
  }

  got = g.Find("handsh", 0, 5)
  if len(got) == 0 || got[0].ID != hs {
    t.Fatalf("prefix handsh: %v", got)
  }

  got = g.Find("handshake", tcp, 5)
  if slices.Contains(ids(got), mention) {
    t.Fatalf("subtree filter let udp's node through: %v", got)
  }

  if len(g.Find("zzz qqq", 0, 5)) != 0 {
    t.Fatal("unrelated query matched")
  }
}

// TestFindSeesChangesBetweenRebuilds: nodes added, rewritten, moved and
// deleted after the index was built are found by their new text and
// place only.
func TestFindSeesChangesBetweenRebuilds(t *testing.T) {
  g := New()
  sample(t, g)
  g.Find("x", 0, 1) // builds the index
  built := g.index.built

  seq, _ := g.Lookup("tcp/seqnum")
  added := need(
    g.AddNode(
      0, "congestion", "cwnd grows by slow start", KindConcept, ConfGuess, 0,
    ),
  )
  mustDo(t, g.SetClaim(seq, "every byte has an ordinal"))
  win, _ := g.Lookup("tcp/window")
  udp := need(g.AddNode(0, "udp", "", KindTopic, ConfGuess, 0))
  mustDo(t, g.Move(win, udp, "window"))
  hs, _ := g.Lookup("tcp/handshake")
  mustDo(t, g.Delete(hs))

  if g.index.built != built {
    t.Fatal("small changes should not rebuild the index")
  }

  got := g.Find("slow start", 0, 5)
  if len(got) == 0 || got[0].ID != added {
    t.Fatalf("added node: %v", got)
  }

  got = g.Find("ordinal", 0, 5)
  if len(got) == 0 || got[0].ID != seq {
    t.Fatalf("rewritten claim: %v", got)
  }

  got = g.Find("numbered", 0, 5)
  if len(got) != 0 {
    t.Fatalf("old claim text still matches: %v", got)
  }

  got = g.Find("flow control", udp, 5)
  if len(got) == 0 || got[0].ID != win {
    t.Fatalf("moved node under its new parent: %v", got)
  }

  tcp, _ := g.Lookup("tcp")
  got = g.Find("flow control", tcp, 5)
  if len(got) != 0 {
    t.Fatalf("moved node still under its old parent: %v", got)
  }

  got = g.Find("SYN", 0, 5)
  if len(got) != 0 {
    t.Fatalf("deleted node found: %v", got)
  }
}

func TestFindRebuildsPastThreshold(t *testing.T) {
  g := New()
  g.Find("x", 0, 1)
  for i := 0; i <= rebuildAt; i++ {
    need(g.AddNode(0, fmt.Sprintf("n%d", i), "filler", KindFact, ConfGuess, 0))
  }

  target := need(g.AddNode(0, "needle", "", KindFact, ConfGuess, 0))
  got := g.Find("needle", 0, 3)
  if g.index.built != g.Len() || len(got) == 0 || got[0].ID != target {
    t.Fatalf("built %d of %d, hits %v", g.index.built, g.Len(), got)
  }
}

func TestFindTopK(t *testing.T) {
  g := New()
  for i := 0; i < 40; i++ {
    claim := "socket"
    for j := 0; j < i % 5; j++ {
      claim += " socket"
    }

    need(g.AddNode(0, fmt.Sprintf("n%02d", i), claim, KindFact, ConfGuess, 0))
  }

  got := g.Find("socket", 0, 100)
  if len(got) != MaxHits {
    t.Fatalf("k should cap at %d, got %d", MaxHits, len(got))
  }

  for i := 1; i < len(got); i++ {
    if got[i].Score > got[i - 1].Score {
      t.Fatalf("not best first: %v", got)
    }
  }
}

func TestSimilar(t *testing.T) {
  g := New()
  sample(t, g)
  tcp, _ := g.Lookup("tcp")
  hs, _ := g.Lookup("tcp/handshake")

  got := g.Similar(tcp, "handshake", "the three segments: SYN, SYN-ACK, ACK")
  if len(got) == 0 || got[0].ID != hs || got[0].Score < SimilarAt {
    t.Fatalf("near-duplicate missed: %v", got)
  }

  if len(
    g.Similar(tcp, "teardown", "FIN and FIN-ACK close a connection"),
  ) != 0 {
    t.Fatal("an unrelated node was called similar")
  }

  // word overlap can't tell these apart: it is held for the model,
  // never merged
  a := need(
    g.AddNode(
      tcp,
      "accept",
      "the server accepts the connection after ACK",
      KindFact,
      ConfGuess,
      0,
    ),
  )
  got = g.Similar(tcp, "reject", "the server rejects the connection after ACK")
  if len(got) == 0 || got[0].ID != a {
    t.Fatalf("one-word difference should be flagged for the model: %v", got)
  }
}

func TestCycles(t *testing.T) {
  g := New()
  n := make([]NodeID, 7)
  for i := range n {
    n[i] = need(
      g.AddNode(0, fmt.Sprintf("c%d", i), "", KindConcept, ConfGuess, 0),
    )
  }

  edge := func(a, b int) { mustDo(t, g.AddEdge(n[a], n[b], EdgeRequires)) }
  edge(0, 1)
  edge(1, 0) // loop {0, 1}
  edge(2, 3)
  edge(3, 4)
  edge(4, 2) // loop {2, 3, 4}
  edge(4, 5) // a tail out of the loop
  edge(5, 6)
  mustDo(t, g.AddEdge(n[6], n[5], EdgeExplains)) // not a requires loop

  got := g.Cycles()
  want := [][]NodeID{ { n[0], n[1] }, { n[2], n[3], n[4] } }
  slices.SortFunc(got, func(a, b []NodeID) int { return int(a[0]) - int(b[0]) })
  if fmt.Sprint(got) != fmt.Sprint(want) {
    t.Fatalf("cycles = %v, want %v", got, want)
  }

  // reframing one edge resolves the loop
  mustDo(t, g.RemoveEdge(n[1], n[0], EdgeRequires))
  mustDo(t, g.AddEdge(n[1], n[0], EdgeExplains))
  got = g.Cycles()
  if len(got) != 1 {
    t.Fatalf("after reframing: %v", got)
  }
}

func TestCyclesLongChainNoRecursion(t *testing.T) {
  g := New()
  prev := need(g.AddNode(0, "c0", "", KindConcept, ConfGuess, 0))
  first := prev
  for i := 1; i < 50000; i++ {
    id := need(
      g.AddNode(0, fmt.Sprintf("c%d", i), "", KindConcept, ConfGuess, 0),
    )
    mustDo(t, g.AddEdge(prev, id, EdgeRequires))
    prev = id
  }

  mustDo(t, g.AddEdge(prev, first, EdgeRequires))
  got := g.Cycles()
  if len(got) != 1 || len(got[0]) != 50000 {
    t.Fatalf("one loop of 50000 expected, got %d loops", len(got))
  }
}

func TestTraceWeakestFirst(t *testing.T) {
  g := New()
  m := need(g.Intern(RoleMinion, "small", "s", 1))
  c := need(g.Intern(RoleCopilot, "big", "s", 0))
  goal := need(g.AddNode(0, "goal", "", KindFact, ConfChecked, c))
  strong := need(g.AddNode(0, "strong", "", KindFact, ConfTested, c))
  guess := need(g.AddNode(0, "guess", "", KindFact, ConfGuess, c))
  minion := need(g.AddNode(0, "minion", "", KindFact, ConfChecked, m))
  deep := need(g.AddNode(0, "deep", "", KindFact, ConfChecked, c))
  verified := need(g.AddNode(0, "verified", "", KindFact, ConfChecked, m))
  mustDo(t, g.SetFlags(verified, FlagVerified, 0))
  for _, to := range []NodeID{ strong, guess, minion, verified } {
    mustDo(t, g.AddEdge(goal, to, EdgeDerivedFrom))
  }

  mustDo(t, g.AddEdge(strong, deep, EdgeDerivedFrom))

  got := g.Trace(goal, 5)
  var order []NodeID
  for _, s := range got {
    order = append(order, s.ID)
  }

  want := []NodeID{ minion, guess, verified, deep, strong }
  if fmt.Sprint(order) != fmt.Sprint(want) {
    t.Fatalf("trace order %v, want %v", order, want)
  }

  if len(g.Trace(goal, 1)) != 4 {
    t.Fatal("depth limit ignored")
  }
}

func TestPlan(t *testing.T) {
  g := New()
  node := func(name string) NodeID {
    return need(g.AddNode(0, name, "", KindConcept, ConfGuess, 0))
  }

  // goal needs window and handshake; both need seqnum, which needs
  // bytes; window also needs buffers; bytes is known already
  goal := node("goal")
  window := node("window")
  hs := node("handshake")
  seq := node("seqnum")
  bytesN := node("bytes")
  buffers := node("buffers")
  req := func(a, b NodeID) { mustDo(t, g.AddEdge(a, b, EdgeRequires)) }
  req(goal, window)
  req(goal, hs)
  req(window, seq)
  req(hs, seq)
  req(seq, bytesN)
  req(window, buffers)

  known := func(id NodeID) bool { return id == bytesN }
  order, frontier, err := g.Plan(goal, known)
  if err != nil {
    t.Fatal(err)
  }

  // seqnum unlocks window, handshake and goal (3); buffers only
  // window and goal (2): seqnum first
  want := []NodeID{ seq, buffers, hs, window, goal }
  if fmt.Sprint(order) != fmt.Sprint(want) || frontier != 2 {
    t.Fatalf("order %v frontier %d, want %v frontier 2", order, frontier, want)
  }

  pos := map[NodeID]int{}
  for i, id := range order {
    pos[id] = i
  }

  for _, e := range [][2]NodeID{
    { goal, window },
    { goal, hs },
    { window, seq },
    { hs, seq },
    { window, buffers },
  } {
    if pos[e[0]] < pos[e[1]] {
      t.Fatalf("%d taught before its prerequisite %d", e[0], e[1])
    }
  }

  order, _, _ = g.Plan(goal, func(id NodeID) bool { return id == goal })
  if len(order) != 0 {
    t.Fatal("a known goal needs no plan")
  }

  req(bytesN, window) // a loop, but through a known node: not in the closure
  _, _, err = g.Plan(goal, known)
  if err != nil {
    t.Fatalf("a loop through known nodes should not block: %v", err)
  }

  req(seq, hs) // hs → seq → hs
  _, _, err = g.Plan(goal, known)
  ce, ok := err.(*CycleError)
  if !ok || !slices.Contains(ce.Nodes, hs) || !slices.Contains(ce.Nodes, seq) {
    t.Fatalf("want a cycle error naming hs and seqnum, got %v", err)
  }
}

func BenchmarkFind(b *testing.B) {
  dir := b.TempDir()
  bigTree(b, dir)
  g := need(Open(dir))
  defer g.Close()
  g.Find("x", 0, 1)
  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    g.Find("concept claim usual", 0, 10)
  }
}

func BenchmarkFindPrefix(b *testing.B) {
  dir := b.TempDir()
  bigTree(b, dir)
  g := need(Open(dir))
  defer g.Close()
  g.Find("x", 0, 1)
  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    g.Find("concept4", 0, 10)
  }
}

func BenchmarkCycles(b *testing.B) {
  dir := b.TempDir()
  bigTree(b, dir)
  g := need(Open(dir))
  defer g.Close()
  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    g.Cycles()
  }
}

func BenchmarkIndexBuild(b *testing.B) {
  dir := b.TempDir()
  bigTree(b, dir)
  g := need(Open(dir))
  defer g.Close()
  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    g.index.build(g)
  }
}
