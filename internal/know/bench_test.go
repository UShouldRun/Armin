package know

import (
  "fmt"
  "os"
  "path/filepath"
  "testing"
)

// bigTree builds the plan's reference size in dir: 10k nodes in a
// 100 × 100 outline, 20k edges, 15k evidence, then compacts it.
func bigTree(tb testing.TB, dir string) {
  g := need(Open(dir))
  defer g.Close()

  var ids []NodeID
  for i := 0; i < 100; i++ {
    top := need(
      g.AddNode(0, fmt.Sprintf("topic%d", i), "", KindTopic, ConfChecked, 0),
    )
    for j := 0; j < 99; j++ {
      id := need(g.AddNode(
        top, fmt.Sprintf("concept%d", j),
        "a claim of about the usual length, saying what this concept " +
          "is and why",
        KindConcept, ConfInferred, 0,
      ))
      ids = append(ids, id)
    }
  }

  for i := 0; i < 20000; i++ {
    a := ids[i * 7919 % len(ids)]
    b := ids[(i * 104729 + i / len(ids) + 1) % len(ids)]
    if a != b {
      err := g.AddEdge(a, b, EdgeKind(i % 5))
      if err != nil {
        tb.Fatal(err)
      }
    }
  }

  for i := 0; i < 15000; i++ {
    need(g.AddEvidence(ids[i % len(ids)], RefFile,
      fmt.Sprintf(
        "internal/pkg%d/file.go:%d-%d", i % 50, i, i + 40,
      ), uint32(i)))
  }

  err := g.Compact()
  if err != nil {
    tb.Fatal(err)
  }
}

func TestBigTreeSize(t *testing.T) {
  if testing.Short() {
    t.Skip()
  }

  dir := t.TempDir()
  bigTree(t, dir)
  st := need(os.Stat(filepath.Join(dir, "graph.snap")))
  t.Logf(
    "snapshot at 10k nodes, 20k edges, 15k evidence: %d KiB", st.Size() / 1024,
  )
  if st.Size() > 2 << 20 {
    t.Fatalf("snapshot is %d bytes, over the planned ~1.8 MB", st.Size())
  }

  g := need(Open(dir))
  defer g.Close()
  if g.Len() != 10001 || g.edges.count() < 19900 {
    t.Fatalf("reopened: %d nodes, %d edges", g.Len(), g.edges.count())
  }
}

func BenchmarkOpen(b *testing.B) {
  dir := b.TempDir()
  bigTree(b, dir)
  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    g := need(Open(dir))
    g.Close()
  }
}

func BenchmarkLookup(b *testing.B) {
  dir := b.TempDir()
  bigTree(b, dir)
  g := need(Open(dir))
  defer g.Close()
  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    _, ok := g.Lookup("topic57/concept42")
    if !ok {
      b.Fatal("missing")
    }
  }
}

func BenchmarkAddNode(b *testing.B) {
  dir := b.TempDir()
  g := need(Open(dir))
  defer g.Close()
  top := need(g.AddNode(0, "t", "", KindTopic, ConfGuess, 0))
  names := make([]string, b.N)
  for i := range names {
    names[i] = fmt.Sprintf("n%d", i)
  }

  b.ResetTimer()
  b.ReportAllocs()
  for i := 0; i < b.N; i++ {
    need(g.AddNode(top, names[i], "claim", KindFact, ConfGuess, 0))
  }
}
