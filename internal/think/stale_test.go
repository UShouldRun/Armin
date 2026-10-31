package think

import (
  "context"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/know"
)

func gitIn(t *testing.T, dir string, args ...string) {
  t.Helper()
  cmd := exec.Command("git", append([]string{
    "-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false",
  }, args...)...)
  out, err := cmd.CombinedOutput()
  if err != nil {
    t.Fatalf("git %v: %v\n%s", args, err, out)
  }
}

// TestRefreshKnow: a committed move is followed, an uncommitted change
// makes its node stale and what rests on it suspect, and a requires
// loop gets its question.
func TestRefreshKnow(t *testing.T) {
  _, err := exec.LookPath("git")
  if err != nil {
    t.Skip("no git")
  }

  root := t.TempDir()
  write := func(name, text string) {
    os.WriteFile(filepath.Join(root, name), []byte(text), 0o644)
  }

  write("a.go", "package a\n\nfunc f() {\n  return\n}\n")
  write("b.go", "package a\n\nfunc g() {\n  return\n}\n")
  gitIn(t, root, "init", "-q")
  gitIn(t, root, "add", ".")
  gitIn(t, root, "commit", "-qm", "one")

  g, err := know.Open(t.TempDir())
  if err != nil {
    t.Fatal(err)
  }

  defer g.Close()

  hash := func(name string, start, end int) uint32 {
    data, _ := os.ReadFile(filepath.Join(root, name))
    return know.RangeHash(know.LineHashes(data, nil)[start - 1:end])
  }

  f, _ := g.AddNode(0, "f", "f returns", know.KindFact, know.ConfChecked, 0)
  gn, _ := g.AddNode(0, "g", "g returns", know.KindFact, know.ConfChecked, 0)
  user, _ := g.AddNode(0, "uses-g", "built on g", know.KindFact, know.ConfChecked, 0)
  g.AddEdge(user, gn, know.EdgeDerivedFrom)
  ef, _ := g.AddEvidence(f, know.RefFile, "a.go:3-5", hash("a.go", 3, 5))
  g.AddEvidence(gn, know.RefFile, "b.go:3-5", hash("b.go", 3, 5))

  ctx := context.Background()
  msg, err := refreshKnow(ctx, root, g)
  if err != nil || msg != "" {
    t.Fatalf("first refresh: %q %v", msg, err)
  }

  // committed: f moves down; working tree: g's body changes
  write("a.go", "package a\n\nimport \"x\"\n\nfunc f() {\n  return\n}\n")
  gitIn(t, root, "commit", "-qam", "two")
  write("b.go", "package a\n\nfunc g() {\n  panic(1)\n}\n")

  msg, err = refreshKnow(ctx, root, g)
  if err != nil {
    t.Fatal(err)
  }

  if !strings.Contains(msg, "1 stale") || !strings.Contains(msg, "1 suspect") ||
    !strings.Contains(msg, "1 references followed moved code") {
    t.Fatalf("second refresh: %q", msg)
  }

  if g.Str(g.Ev(ef).Ref) != "a.go:5-7" || g.Node(gn).Flags & know.FlagStale == 0 ||
    g.Node(user).Flags & know.FlagSuspect == 0 {
    t.Fatal("flags or reference wrong")
  }

  // a loop appears between sessions
  g.AddEdge(f, user, know.EdgeRequires)
  g.AddEdge(user, f, know.EdgeRequires)
  msg, _ = refreshKnow(ctx, root, g)
  _, ok := g.Lookup("cycles")
  if !strings.Contains(msg, "1 open requires loops") || !ok {
    t.Fatalf("loop refresh: %q", msg)
  }
}
