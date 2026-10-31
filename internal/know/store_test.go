package know

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestOpenReplaysTheLog(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  sample(t, g)
  want := dumpAll(t, g)
  mustDo(t, g.Close())

  g = need(Open(dir))
  defer g.Close()
  got := dumpAll(t, g)
  if got != want {
    t.Fatalf("after reopening:\n%s\nwant:\n%s", got, want)
  }
}

func TestCompactRoundTrip(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  sample(t, g)

  // garbage for compaction to drop: a rewritten claim, removed
  // evidence, a removed edge, a deleted node
  hs, _ := g.Lookup("tcp/handshake")
  win, _ := g.Lookup("tcp/window")
  mustDo(t, g.SetClaim(hs, "three segments"))
  mustDo(t, g.RemoveEvidence(g.EvidenceOf(hs, nil)[0]))
  mustDo(t, g.RemoveEdge(win, hs, EdgeDerivedFrom))
  x := need(g.AddNode(0, "x", "gone", KindFact, ConfGuess, 0))
  mustDo(t, g.Delete(x))

  want := dumpAll(t, g)
  mustDo(t, g.Compact())
  if g.mapped == nil || len(g.moreNodes) != 0 {
    t.Fatal("compaction should reload from the snapshot")
  }

  if got := dumpAll(t, g); got != want {
    t.Fatalf("after compaction:\n%s\nwant:\n%s", got, want)
  }

  // changes after compaction land in the new log, on top of the
  // mmapped snapshot
  mustDo(t, g.SetClaim(win, "flow control, rwnd"))
  seq, _ := g.Lookup("tcp/seqnum")
  mustDo(t, g.AddEdge(seq, win, EdgeExplains))
  mustDo(t, g.RemoveEdge(win, seq, EdgeRequires))
  want = dumpAll(t, g)
  mustDo(t, g.Close())

  g = need(Open(dir))
  defer g.Close()
  if got := dumpAll(t, g); got != want {
    t.Fatalf("after reopening:\n%s\nwant:\n%s", got, want)
  }

  if g.Live(x) {
    t.Fatal("deleted node came back")
  }
}

// TestTornLog cuts the log mid-record: Open keeps everything before
// the cut, drops the rest, and appends after the good part.
func TestTornLog(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  sample(t, g)
  want := dumpAll(t, g)
  need(g.AddNode(0, "late", "this record gets torn", KindFact, ConfGuess, 0))
  mustDo(t, g.Close())

  path := filepath.Join(dir, "graph.log")
  st := need(os.Stat(path))
  mustDo(t, os.Truncate(path, st.Size() - 5))

  g = need(Open(dir))
  if got := dumpAll(t, g); got != want {
    t.Fatalf("after a torn write:\n%s\nwant:\n%s", got, want)
  }

  need(g.AddNode(0, "after", "", KindFact, ConfGuess, 0))
  mustDo(t, g.Close())

  g = need(Open(dir))
  defer g.Close()
  _, ok := g.Lookup("after")
  _, torn := g.Lookup("late")
  if !ok || torn {
    t.Fatalf("after: %v, late: %v", ok, torn)
  }
}

func TestCorruptRecordStopsReplay(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  sample(t, g)
  want := dumpAll(t, g)
  need(g.AddNode(0, "late", "flipped", KindFact, ConfGuess, 0))
  mustDo(t, g.Close())

  path := filepath.Join(dir, "graph.log")
  data := need(os.ReadFile(path))
  i := strings.LastIndex(string(data), "flipped")
  data[i] ^= 0x20
  mustDo(t, os.WriteFile(path, data, 0o600))

  g = need(Open(dir))
  defer g.Close()
  if got := dumpAll(t, g); got != want {
    t.Fatalf("a bad CRC should end replay:\n%s", got)
  }
}

func TestSecondOpenIsReadOnly(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  defer g.Close()
  sample(t, g)
  mustDo(t, g.Sync())

  r := need(Open(dir))
  defer r.Close()
  if !r.ReadOnly() {
    t.Fatal("a second open should be read-only")
  }

  _, ok := r.Lookup("tcp/handshake")
  if !ok {
    t.Fatal("the read-only copy should see the log")
  }

  _, err := r.AddNode(0, "x", "", KindFact, ConfGuess, 0)
  if err != ErrReadOnly {
    t.Fatalf("write on read-only: %v", err)
  }
}

// TestStaleLogAfterCrashedCompaction: the new snapshot was renamed in
// but the crash came before the new log; the old log's ops are already
// in the snapshot and must not apply twice.
func TestStaleLogAfterCrashedCompaction(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  sample(t, g)
  want := dumpAll(t, g)
  oldLog := need(os.ReadFile(filepath.Join(dir, "graph.log")))
  mustDo(t, g.Compact())
  mustDo(t, g.Close())

  mustDo(t, os.WriteFile(filepath.Join(dir, "graph.log"), oldLog, 0o600))
  g = need(Open(dir))
  defer g.Close()
  if got := dumpAll(t, g); got != want {
    t.Fatalf("stale log applied:\n%s", got)
  }
}

func TestCorruptSnapshotIsRefused(t *testing.T) {
  dir := t.TempDir()
  g := need(Open(dir))
  sample(t, g)
  mustDo(t, g.Compact())
  mustDo(t, g.Close())

  path := filepath.Join(dir, "graph.snap")
  data := need(os.ReadFile(path))
  data[len(data) - 3] ^= 0xff
  mustDo(t, os.WriteFile(path, data, 0o600))

  _, err := Open(dir)
  if err == nil || !strings.Contains(err.Error(), "checksum") {
    t.Fatalf("want a checksum error, got %v", err)
  }
}
