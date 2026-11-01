package know

import (
  "bytes"
  "fmt"
  "os/exec"
  "strings"
  "testing"
)

func TestCheckFile(t *testing.T) {
  g := New()
  a := need(g.AddNode(0, "a", "", KindFact, ConfChecked, 0))
  b := need(g.AddNode(0, "b", "", KindFact, ConfChecked, 0))
  old := []byte(
    "one\ntwo\nfunc f() {\n  return 1\n}\nthree\nfunc g() {\n  return 2\n}\n",
  )
  hs := LineHashes(old, nil)
  ea := need(g.AddEvidence(a, RefFile, "x.go:3-5", RangeHash(hs[2:5])))
  need(g.AddEvidence(b, RefFile, "x.go:7-9", RangeHash(hs[6:9])))

  // f moves down two lines, g's body changes
  now := []byte(
    "new\nnew\none\ntwo\nfunc f() {\n  return 1\n}\nthree\nfunc g() {\n  " +
      "return 3\n}\n",
  )
  stale, moved, err := g.CheckFile("x.go", now, false, g.CitedFiles()["x.go"])
  if err != nil {
    t.Fatal(err)
  }

  if moved != 1 || len(stale) != 1 || stale[0] != b {
    t.Fatalf("moved %d, stale %v", moved, stale)
  }

  if g.Str(g.Ev(ea).Ref) != "x.go:5-7" || g.Node(a).Flags & FlagStale != 0 {
    t.Fatalf("f's evidence: %q", g.Str(g.Ev(ea).Ref))
  }

  // the file is deleted: everything citing it goes stale
  stale, _, _ = g.CheckFile("x.go", nil, true, g.CitedFiles()["x.go"])
  if len(stale) != 1 || stale[0] != a {
    t.Fatalf("after deletion: %v", stale)
  }
}

func TestMarkSuspect(t *testing.T) {
  g := New()
  n := make([]NodeID, 6)
  for i := range n {
    n[i] = need(
      g.AddNode(0, fmt.Sprintf("c%d", i), "", KindFact, ConfChecked, 0),
    )
  }

  // 1 requires 0, 2 derived from 1, 3 requires 2, 4 explains 0 (not a
  // dependency), 5 requires 3 but is already stale
  mustDo(t, g.AddEdge(n[1], n[0], EdgeRequires))
  mustDo(t, g.AddEdge(n[2], n[1], EdgeDerivedFrom))
  mustDo(t, g.AddEdge(n[3], n[2], EdgeRequires))
  mustDo(t, g.AddEdge(n[4], n[0], EdgeExplains))
  mustDo(t, g.AddEdge(n[5], n[3], EdgeRequires))
  mustDo(t, g.SetFlags(n[5], FlagStale, 0))
  mustDo(t, g.SetFlags(n[0], FlagStale, 0))

  count := need(g.MarkSuspect([]NodeID{ n[0] }))
  if count != 3 {
    t.Fatalf("marked %d, want 3", count)
  }

  for i, want := range []bool{ false, true, true, true, false, false } {
    if (g.Node(n[i]).Flags & FlagSuspect != 0) != want {
      t.Errorf("c%d suspect = %v", i, !want)
    }
  }
}

func TestFlagCycles(t *testing.T) {
  g := New()
  a := need(g.AddNode(0, "a", "", KindConcept, ConfGuess, 0))
  b := need(g.AddNode(0, "b", "", KindConcept, ConfGuess, 0))
  mustDo(t, g.AddEdge(a, b, EdgeRequires))
  mustDo(t, g.AddEdge(b, a, EdgeRequires))

  loops := need(g.FlagCycles(0))
  q, ok := g.Lookup("cycles/loop-1-2")
  if len(loops) != 1 || !ok || g.Node(q).Flags & FlagCycle == 0 ||
    !g.HasEdge(q, a, EdgeExplains) {
    t.Fatalf("loops %v, question %v", loops, ok)
  }

  // a second pass records nothing new
  need(g.FlagCycles(0))
  dir, _ := g.Lookup("cycles")
  if len(g.Children(dir, nil)) != 1 {
    t.Fatal("the same loop was recorded twice")
  }

  if len(g.Cycles()) != 1 {
    t.Fatal("the question node must not join the loop")
  }

  // reframed: the loop and its question go away
  mustDo(t, g.RemoveEdge(b, a, EdgeRequires))
  mustDo(t, g.AddEdge(b, a, EdgeExplains))
  loops = need(g.FlagCycles(0))
  _, still := g.Lookup("cycles")
  if len(loops) != 0 || g.Live(q) || still {
    t.Fatalf("after resolving: loops %v, question live %v", loops, g.Live(q))
  }
}

// TestCheckFileOnRealEdits cites every 10-line window of armin's most
// edited files in each older revision and checks it against the next revision:
// a window whose text still appears must be found (moved or in place),
// and one whose text is gone must be stale, never moved somewhere
// else. Skipped outside the repository.
func TestCheckFileOnRealEdits(t *testing.T) {
  pairs := 0
  for _, path := range []string{
    "internal/config/config.go", "internal/agent/loop.go", "cmd/armin/main.go",
    "internal/llm/client.go", "internal/tools/runcommand.go",
    "internal/sandbox/sandbox.go",
  } {
    out, err := exec.Command(
      "git", "log", "--format=%H", "-n", "13", "--", "../../" + path,
    ).Output()
    revs := strings.Fields(string(out))
    if err != nil || len(revs) < 2 {
      t.Skip("no git history here")
    }

    pairs += len(revs) - 1
    replayFile(t, path, revs)
  }

  t.Logf("%d revision pairs replayed", pairs)
}

func replayFile(t *testing.T, path string, revs []string) {

  show := func(rev string) []byte {
    data, err := exec.Command("git", "show", rev + ":" + path).Output()
    if err != nil {
      t.Skip(err)
    }

    return data
  }

  checked, movedTotal, staleTotal := 0, 0, 0
  for i := len(revs) - 1; i > 0; i-- {
    old, now := show(revs[i]), show(revs[i - 1])
    oldLines := bytes.SplitAfter(old, []byte("\n"))
    nowText := string(now)
    hs := LineHashes(old, nil)
    g := New()
    type window struct {
      ev    EvidenceID
      text  string
      start int
    }

    var ws []window
    for start := 0; start + 10 <= len(hs); start += 7 {
      id := need(
        g.AddNode(0, fmt.Sprintf("w%d", start), "", KindFact, ConfChecked, 0),
      )
      ref := fmt.Sprintf("%s:%d-%d", path, start + 1, start + 10)
      ev := need(
        g.AddEvidence(id, RefFile, ref, RangeHash(hs[start:start + 10])),
      )
      ws = append(
        ws,
        window{
          ev:    ev,
          text:  string(bytes.Join(oldLines[start:start + 10], nil)),
          start: start,
        },
      )
    }

    _, moved, err := g.CheckFile(path, now, false, g.CitedFiles()[path])
    if err != nil {
      t.Fatal(err)
    }

    movedTotal += moved
    nowLines := bytes.SplitAfter(now, []byte("\n"))
    for _, w := range ws {
      checked++
      e := g.Ev(w.ev)
      isStale := g.Node(e.Node).Flags & FlagStale != 0
      // whole lines only: "\n" + text in "\n" + file, the last line
      // allowed to lose its newline at the end of the file
      body := strings.TrimSuffix(w.text, "\n")
      present := strings.Contains("\n" + nowText, "\n" + w.text) ||
        strings.HasSuffix("\n" + nowText, "\n" + body)
      if isStale {
        staleTotal++
      }

      if present == isStale {
        t.Fatalf("%s → %s, window at %d: present %v but stale %v",
          revs[i][:7], revs[i - 1][:7], w.start + 1, present, isStale)
      }

      if !isStale {
        _, s, end, _ := ParseRange(g.Str(e.Ref))
        got := string(bytes.Join(nowLines[s - 1:end], nil))
        if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(w.text, "\n") {
          t.Fatalf(
            "window at %d points at other text after the check", w.start + 1,
          )
        }
      }
    }
  }

  t.Logf("%s: %d windows over %d revisions: %d moved, %d stale",
    path, checked, len(revs) - 1, movedTotal, staleTotal)
}
