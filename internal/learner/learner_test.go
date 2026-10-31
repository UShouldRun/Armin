package learner

import (
  "fmt"
  "os"
  "path/filepath"
  "testing"
  "time"

  "armin/internal/know"
)

var start = time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

func days(n int) time.Time { return start.AddDate(0, 0, n) }

func concept(
  t *testing.T, l *Learner, parent know.NodeID, name string,
) know.NodeID {
  t.Helper()
  id, err := l.Tree.AddNode(
    parent, name, name + " is a thing", know.KindConcept, know.ConfChecked, 0,
  )
  if err != nil {
    t.Fatal(err)
  }

  return id
}

func answer(
  t *testing.T, l *Learner, id know.NodeID, r Result, when time.Time,
) {
  t.Helper()
  err := l.Record(id, EventQuiz, r, SureMostly, when)
  if err != nil {
    t.Fatal(err)
  }
}

func TestLeitnerAndFading(t *testing.T) {
  l := New()
  c := concept(t, l, 0, "tcp")
  if l.State(c, Day(start)) != Unknown {
    t.Fatal("a new concept should be unknown")
  }

  for i := 0; i < 3; i++ {
    answer(t, l, c, Right, days(i))
  }

  if l.Box(c) != 3 || l.State(c, Day(days(2))) != Known {
    t.Fatalf("box %d", l.Box(c))
  }

  // box 3 is trusted 8 days after the last answer
  if l.State(c, Day(days(10))) != Known ||
      l.State(c, Day(days(11))) != NotSure {
    t.Fatal("fading at 2^box days")
  }

  answer(t, l, c, Wrong, days(11))
  if l.Box(c) != 0 || l.State(c, Day(days(11))) != Unknown {
    t.Fatal("a wrong answer resets the box")
  }
}

func TestPrerequisitesByImplicationAndUse(t *testing.T) {
  l := New()
  add := concept(t, l, 0, "addition")
  mul := concept(t, l, 0, "multiplication")
  exp := concept(t, l, 0, "powers")
  l.Tree.AddEdge(mul, add, know.EdgeRequires)
  l.Tree.AddEdge(exp, mul, know.EdgeRequires)

  answer(t, l, exp, Right, days(0))
  if l.State(mul, Day(days(0))) != Implied ||
      l.State(add, Day(days(0))) != Implied {
    t.Fatal(
      "prerequisites of a right answer are known by implication, at any depth",
    )
  }

  // addition answered on its own, then left alone: using it in a
  // multiplication keeps it from fading
  answer(t, l, add, Right, days(1)) // box 1: 2 days
  answer(t, l, mul, Right, days(10))
  if l.State(add, Day(days(11))) != Known {
    t.Fatal("using a prerequisite should refresh it")
  }

  if l.State(add, Day(days(13))) != NotSure {
    t.Fatal("it still fades from the refreshed day")
  }
}

func TestCrystallizedDeclaredAndScore(t *testing.T) {
  l := New()
  c := concept(t, l, 0, "counting")
  err := l.Record(c, EventSelfReport, ReportExplain, SureCertain, days(0))
  if err != nil {
    t.Fatal(err)
  }

  score, ok := l.Score(c)
  if !ok || score != 50 || l.State(c, Day(days(3000))) != Known {
    t.Fatalf("declared: score %d crystal %v", score, ok)
  }

  // 50 → 43 → 37 → 32 → 28: four wrong in a row reach the refresher
  for i, want := range []int{ 43, 37, 32, 28 } {
    answer(t, l, c, Wrong, days(i + 1))
    score, _ = l.Score(c)
    if score != want {
      t.Fatalf("after %d wrong: %d, want %d", i + 1, score, want)
    }
  }

  if l.State(c, Day(days(5))) != NotSure || len(l.Refreshers()) != 1 {
    t.Fatal("below 30: not sure, and a refresher is recommended")
  }

  answer(t, l, c, Right, days(6)) // 28 + ceil(22 / 8) = 31
  score, _ = l.Score(c)
  if score != 31 || l.State(c, Day(days(6))) != Known ||
      len(l.Refreshers()) != 0 {
    t.Fatalf("after the refresher: %d", score)
  }
}

func TestMoveScoreNeverSticks(t *testing.T) {
  if moveScore(49, 50) != 50 || moveScore(1, 0) != 0 ||
      moveScore(50, 50) != 50 {
    t.Fatal("the score must reach its ends")
  }

  for s := 0; s <= 50; s++ {
    if s < 50 && moveScore(s, 50) <= s || s > 0 && moveScore(s, 0) >= s {
      t.Fatalf("stuck at %d", s)
    }
  }
}

func TestCrystallizedEarned(t *testing.T) {
  l := New()
  c := concept(t, l, 0, "sockets")
  answer(t, l, c, Wrong, days(0))
  for i := 1; i <= 7; i++ {
    answer(t, l, c, Right, days(i))
  }

  if l.Box(c) != 7 {
    t.Fatalf("box %d", l.Box(c))
  }

  answer(t, l, c, Right, days(7 + 100)) // too early
  _, ok := l.Score(c)
  if ok {
    t.Fatal("crystallized before the full 128 days")
  }

  answer(t, l, c, Right, days(107 + 128))
  score, ok := l.Score(c)
  // 9 right of 10 answers so far, the last one included: 45
  if !ok || score != 45 {
    t.Fatalf("earned: score %d crystal %v", score, ok)
  }
}

func TestSelfReportLevels(t *testing.T) {
  l := New()
  for level, want := range map[Result]State{
    ReportNever: Unknown, ReportHeard: Unknown, ReportUsed: Known,
    ReportExplain: Known,
  } {
    c := concept(t, l, 0, fmt.Sprintf("c%d", level))
    l.Record(c, EventSelfReport, level, SureMostly, days(0))
    if l.State(c, Day(days(0))) != want {
      t.Errorf("level %d: %v, want %v", level, l.State(c, Day(days(0))), want)
    }

    _, crystal := l.Score(c)
    if crystal {
      t.Errorf("level %d crystallized without certainty", level)
    }
  }
}

func TestPersistence(t *testing.T) {
  dir := t.TempDir()
  l, err := Open(dir)
  if err != nil {
    t.Fatal(err)
  }

  c := concept(t, l, 0, "dns")
  for i := 0; i < 4; i++ {
    answer(t, l, c, Right, days(i))
  }

  l.Close()

  // a torn record at the end is dropped
  f, _ := os.OpenFile(
    filepath.Join(dir, "events"), os.O_WRONLY | os.O_APPEND, 0,
  )
  f.Write([]byte{ 1, 2, 3 })
  f.Close()

  l, err = Open(dir)
  if err != nil {
    t.Fatal(err)
  }

  defer l.Close()
  if l.Box(c) != 4 || len(l.events) != 4 {
    t.Fatalf("after reopening: box %d, %d events", l.Box(c), len(l.events))
  }

  answer(t, l, c, Right, days(5))
  st, _ := os.Stat(filepath.Join(dir, "events"))
  if st.Size() != 5 * eventSize {
    t.Fatalf("events file is %d bytes", st.Size())
  }
}

// TestProbeChain: a chain of 16 prerequisites, every possible boundary
// of what the user knows, found in at most 5 questions.
func TestProbeChain(t *testing.T) {
  l := New()
  chain := make([]know.NodeID, 16)
  for i := range chain {
    chain[i] = concept(t, l, 0, fmt.Sprintf("c%d", i))
    if i > 0 {
      l.Tree.AddEdge(chain[i], chain[i - 1], know.EdgeRequires)
    }
  }

  for knows := 0; knows <= len(chain); knows++ {
    p := l.NewProbe(chain)
    asked := 0
    for {
      id, ok := p.Next()
      if !ok {
        break
      }

      asked++
      p.Answer(id, int(id - chain[0]) < knows)
    }

    if asked > 5 {
      t.Fatalf("knows %d: %d questions", knows, asked)
    }

    for i, id := range chain {
      got, settled := p.Known(id)
      if !settled || got != (i < knows) {
        t.Fatalf("knows %d: c%d known %v settled %v", knows, i, got, settled)
      }
    }
  }
}

// TestProbeDAG: a diamond under a goal, the user knows one side; every
// concept ends settled, correctly, in fewer questions than concepts.
func TestProbeDAG(t *testing.T) {
  l := New()
  base := concept(t, l, 0, "bytes")
  seq := concept(t, l, 0, "seqnum")
  buf := concept(t, l, 0, "buffers")
  win := concept(t, l, 0, "window")
  goal := concept(t, l, 0, "congestion")
  for _, e := range [][2]know.NodeID{
    { seq, base }, { buf, base }, { win, seq }, { win, buf }, { goal, win },
  } {
    l.Tree.AddEdge(e[0], e[1], know.EdgeRequires)
  }

  knows := map[know.NodeID]bool{ base: true, seq: true }
  nodes := []know.NodeID{ base, seq, buf, win, goal }
  p := l.NewProbe(nodes)
  asked := 0
  for {
    id, ok := p.Next()
    if !ok {
      break
    }

    asked++
    p.Answer(id, knows[id])
  }

  for _, id := range nodes {
    got, settled := p.Known(id)
    if !settled || got != knows[id] {
      t.Fatalf("%d: known %v settled %v", id, got, settled)
    }
  }

  if asked >= len(nodes) {
    t.Fatalf("%d questions for %d concepts", asked, len(nodes))
  }
}

func TestAreaReport(t *testing.T) {
  l := New()
  area := concept(t, l, 0, "networking")
  a := concept(t, l, area, "tcp")
  b := concept(t, l, a, "handshake")

  under, err := l.AreaReport(area, ReportExplain, SureMostly, days(0))
  if err != nil || under != nil {
    t.Fatal(err)
  }

  if l.State(a, Day(days(0))) != Implied ||
      l.State(b, Day(days(0))) != Implied {
    t.Fatal("could explain: the area's concepts are known by implication")
  }

  other := concept(t, l, 0, "graphics")
  c := concept(t, l, other, "shaders")
  under, _ = l.AreaReport(other, ReportUsed, SureMostly, days(0))
  if len(under) != 1 || under[0] != c {
    t.Fatalf("used it: the concepts to probe, got %v", under)
  }
}

func TestGapPutsInDoubt(t *testing.T) {
  l := New()
  c := concept(t, l, 0, "window")
  answer(t, l, c, Right, days(0))
  answer(t, l, c, Right, days(1))
  l.Record(c, EventExplanation, Partial, SureMostly, days(2))
  if l.State(c, Day(days(2))) != NotSure || l.Box(c) != 2 {
    t.Fatal("a gap puts a known concept in doubt, without dropping its box")
  }

  answer(t, l, c, Right, days(3))
  if l.State(c, Day(days(3))) != Known {
    t.Fatal("a right answer clears the doubt")
  }
}
