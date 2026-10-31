package think

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"

  "armin/internal/know"
)

// TestRefreshKnow runs on a plain folder, no version control: a move
// is followed, an edit makes its node stale and what rests on it
// suspect, a deleted file goes stale, an untouched file isn't read,
// a file replaced with the same size and mtime is still caught, and a
// requires loop gets its question.
func TestRefreshKnow(t *testing.T) {
  root := t.TempDir()
  write := func(name, text string) {
    os.WriteFile(filepath.Join(root, name), []byte(text), 0o644)
  }

  write("a.go", "package a\n\nfunc f() {\n  return\n}\n")
  write("b.go", "package a\n\nfunc g() {\n  return\n}\n")
  write("c.go", "package a\n\nfunc h() {\n  return\n}\n")
  write("d.go", "package a\n\nfunc k() {\n  return 1\n}\n")

  g, err := know.Open(t.TempDir())
  if err != nil {
    t.Fatal(err)
  }

  defer g.Close()

  hash := func(name string, start, end int) uint32 {
    data, _ := os.ReadFile(filepath.Join(root, name))
    return know.RangeHash(know.LineHashes(data, nil)[start - 1:end])
  }

  node := func(name string) know.NodeID {
    id, _ := g.AddNode(0, name, name + " holds", know.KindFact, know.ConfChecked, 0)
    return id
  }

  f, gn, h, k := node("f"), node("g"), node("h"), node("k")
  user := node("uses-g")
  g.AddEdge(user, gn, know.EdgeDerivedFrom)
  ef, _ := g.AddEvidence(f, know.RefFile, "a.go:3-5", hash("a.go", 3, 5))
  g.AddEvidence(gn, know.RefFile, "b.go:3-5", hash("b.go", 3, 5))
  eh, _ := g.AddEvidence(h, know.RefFile, "c.go:3-5", hash("c.go", 3, 5))
  g.AddEvidence(k, know.RefFile, "d.go:3-5", hash("d.go", 3, 5))

  msg, err := refreshKnow(root, g)
  if err != nil || msg != "" {
    t.Fatalf("first refresh: %q %v", msg, err)
  }

  // c.go is untouched; its evidence is made wrong on purpose, so a
  // re-read would mark h stale: the stamp must spare it
  g.Reanchor(eh, "c.go:3-5", 12345)

  // f moves down, g's body changes
  write("a.go", "package a\n\nimport \"x\"\n\nfunc f() {\n  return\n}\n")
  write("b.go", "package a\n\nfunc g() {\n  panic(1)\n}\n")

  // d.go is replaced by same-size text with its old mtime put back
  st, _ := os.Stat(filepath.Join(root, "d.go"))
  time.Sleep(10 * time.Millisecond)
  write("d.go", "package a\n\nfunc k() {\n  return 2\n}\n")
  os.Chtimes(filepath.Join(root, "d.go"), st.ModTime(), st.ModTime())

  msg, err = refreshKnow(root, g)
  if err != nil {
    t.Fatal(err)
  }

  if !strings.Contains(msg, "2 stale") || !strings.Contains(msg, "1 suspect") ||
    !strings.Contains(msg, "1 references followed moved code") {
    t.Fatalf("second refresh: %q", msg)
  }

  if g.Str(g.Ev(ef).Ref) != "a.go:5-7" || g.Node(gn).Flags & know.FlagStale == 0 ||
    g.Node(user).Flags & know.FlagSuspect == 0 || g.Node(k).Flags & know.FlagStale == 0 {
    t.Fatal("flags or reference wrong")
  }

  if g.Node(h).Flags & know.FlagStale != 0 {
    t.Fatal("an untouched file was re-read")
  }

  // a deleted file, and a loop between sessions
  os.Remove(filepath.Join(root, "a.go"))
  g.AddEdge(h, user, know.EdgeRequires)
  g.AddEdge(user, h, know.EdgeRequires)
  msg, _ = refreshKnow(root, g)
  _, ok := g.Lookup("cycles")
  if !strings.Contains(msg, "1 stale") || !strings.Contains(msg, "1 open requires loops") || !ok {
    t.Fatalf("third refresh: %q", msg)
  }
}

func TestStampsRoundTrip(t *testing.T) {
  path := filepath.Join(t.TempDir(), "stamps")
  in := map[string]stamp{
    "a.go":     { Size: 1, Mtime: 2, Ctime: 3, Ino: 4 },
    "dir/b.go": { Size: 5, Mtime: 6, Ctime: 7, Ino: 8 },
  }

  err := writeStamps(path, in)
  if err != nil {
    t.Fatal(err)
  }

  out := readStamps(path)
  if len(out) != 2 || out["dir/b.go"] != in["dir/b.go"] {
    t.Fatalf("round trip: %v", out)
  }

  os.WriteFile(path, []byte{ 9, 200 }, 0o600)
  if len(readStamps(path)) != 0 {
    t.Fatal("a damaged file should read as empty")
  }
}
