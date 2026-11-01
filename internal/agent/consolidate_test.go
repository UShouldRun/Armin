package agent

import (
  "context"
  "net/http"
  "os"
  "path/filepath"
  "strings"
  "testing"

  "armin/internal/know"
  "armin/internal/llm"
)

// pointerLoop is a Loop on a temp project with a recorded pointer: the
// question "how are sessions saved?" and a read of store.go:1-4.
func pointerLoop(t *testing.T, reply string) (*Loop, know.NodeID) {
  t.Helper()
  l, root := newTestLoop(t, func(w http.ResponseWriter, r *http.Request) {
    writeChatResponse(t, w, llm.Message{ Role: "assistant", Content: reply })
  })
  os.MkdirAll(filepath.Join(root, "internal", "session"), 0o755)
  os.WriteFile(filepath.Join(root, "internal", "session", "store.go"),
    []byte("package session\n\n// Append writes new messages only.\n"+
      "func Append() {}\n"), 0o644)

  l.Know = know.New()
  l.Session = "s1"
  minion := l.LLM
  l.Minion = func(context.Context) (*llm.Client, error) { return minion, nil }
  author, _ := l.Know.Intern(know.RoleArmin, "", "s1", 0)
  dir, _ := l.Know.AddNode(
    0, "sessions", "", know.KindTopic, know.ConfChecked, author,
  )
  id, _ := l.Know.AddNode(dir, "how-are-sessions-saved",
    "internal/session/store.go:1-4", know.KindFile, know.ConfChecked, author)
  l.addEvidence(id, "internal/session/store.go:1-4")
  question := questionPrefix + "how are sessions saved?"
  l.Know.AddEvidence(id, know.RefSession, question, 0)

  return l, id
}

func TestConsolidateWritesClaims(t *testing.T) {
  l, pointer := pointerLoop(t,
    "Sessions are appended, never rewritten @ internal/session/store.go:1-4\n"+
      "Made up @ internal/session/other.go:1-9\n")

  n, err := l.Consolidate(context.Background(), 5)
  if err != nil || n != 1 {
    t.Fatalf("consolidated %d, %v", n, err)
  }

  id, ok := l.Know.Lookup("session/how-are-sessions-saved")
  if !ok {
    t.Fatal("the claim wasn't written under the cited file's package")
  }

  node := l.Know.Node(id)
  author := l.Know.Author(node.Author)
  if node.Kind() != know.KindFact || node.Conf() != know.ConfInferred ||
    author.Role != know.RoleMinion ||
    !strings.Contains(l.Know.Claim(id), "appended") {
    t.Fatalf("claim: %+v %q", node, l.Know.Claim(id))
  }

  if !l.Know.HasEdge(id, pointer, know.EdgeDerivedFrom) {
    t.Fatal("the claim should be derived from the pointer")
  }

  evs := l.Know.EvidenceOf(id, nil)
  if len(evs) != 1 || l.Know.Ev(evs[0]).Hash == 0 {
    t.Fatal("the claim should carry its range as hashed evidence")
  }

  // the made-up range was dropped: one claim only
  if len(l.Know.Children(node.Parent, nil)) != 1 {
    t.Fatal("a claim citing a range not shown was kept")
  }

  if len(l.pendingPointers(5)) != 0 {
    t.Fatal("a consolidated pointer is still pending")
  }
}

func TestPointerSourceWholeFile(t *testing.T) {
  l, id := pointerLoop(t, "")
  l.Know.AddEvidence(id, know.RefSession, "internal/session/store.go", 0)

  _, excerpts := l.pointerSource(id)
  last := excerpts[len(excerpts) - 1]
  if last.header != "internal/session/store.go:1-4" {
    t.Fatalf("a whole-file read of a 4-line file: %q", last.header)
  }

  _, err := l.rangeHash("internal/session/store.go", 1, 4)
  if err != nil {
    t.Fatal(err)
  }
}

func TestConsolidateLinksASimilarClaim(t *testing.T) {
  l, pointer := pointerLoop(t,
    "sessions are appended never rewritten @ internal/session/store.go:1-4\n")
  area, _ := l.Know.AddNode(
    0, "session", "", know.KindTopic, know.ConfChecked, 0,
  )
  have, _ := l.Know.AddNode(area, "appending",
    "sessions are appended, never rewritten",
    know.KindFact, know.ConfChecked, 0)

  l.Consolidate(context.Background(), 5)
  if !l.Know.HasEdge(have, pointer, know.EdgeDerivedFrom) {
    t.Fatal("the similar claim should be linked to the pointer")
  }

  if len(l.Know.Children(area, nil)) != 1 {
    t.Fatal("a near-duplicate claim was added")
  }
}

func TestConsolidateNothingUsable(t *testing.T) {
  l, _ := pointerLoop(t, "I can't tell from this.")
  n, err := l.Consolidate(context.Background(), 5)
  if err != nil || n != 0 || len(l.pendingPointers(5)) != 1 {
    t.Fatalf("n %d, err %v: an unusable reply must leave it pending", n, err)
  }
}

func TestUserTextAndSlug(t *testing.T) {
  msg := "[armin] Your role is now advisor: x.\n\n" + recallHead +
    "n2 a/b fact checked\n\nhow are sessions saved?"
  if userText(msg) != "how are sessions saved?" {
    t.Fatalf("userText = %q", userText(msg))
  }

  if userText("[armin] stepping back after repeated failures") != "" {
    t.Fatal("armin's own note isn't the user's question")
  }

  got := turnSlug("--> can you talk to Claude?")
  if got != "talk-claude" {
    t.Fatalf("turnSlug = %q", got)
  }
}

func TestUserTextCleansWhatWasAdded(t *testing.T) {
  cases := []struct{ in, want string }{
    { "fix the dirs\n\n(How to approach this fix task: First...)",
      "fix the dirs" },
    { "[00:00:01.000 --> 00:00:18.000] Hey, how does it work?",
      "Hey, how does it work?" },
    { "why is the logo text\n\n" + strings.Repeat("tX0gKpVK", 10),
      "why is the logo text" },
    { summaryMark + ", compacted to save context]\n# brief", "" },
  }

  for _, c := range cases {
    got := userText(c.in)
    if got != c.want {
      t.Errorf("userText(%.30q) = %q, want %q", c.in, got, c.want)
    }
  }

  got := turnSlug("ok, the file tree is correct now, but the dirs")
  if got != "file-tree-correct-dirs" {
    t.Fatalf("turnSlug = %q", got)
  }
}
