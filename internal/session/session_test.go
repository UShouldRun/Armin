package session

import (
  "encoding/json"
  "os"
  "path/filepath"
  "regexp"
  "strings"
  "testing"
  "time"

  "armin/internal/llm"
)

// withDataDir points XDG_DATA_HOME at a temp dir so tests never
// touch the real ~/.local/share/armin.
func withDataDir(t *testing.T) {
  t.Helper()
  t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func sampleHistory() []llm.Message {
  return []llm.Message{
    { Role: "system", Content: "you are armin" },
    { Role: "user", Content: "hi" },
    { Role: "assistant", Content: "hello" },
  }
}

func newMeta() *Meta {
  return &Meta{ ID: NewID(), Root: "/tmp/project", Model: "local" }
}

func lineCount(t *testing.T, id string) int {
  t.Helper()
  path, err := pathFor(id)
  if err != nil {
    t.Fatal(err)
  }

  raw, err := os.ReadFile(path)
  if err != nil {
    t.Fatal(err)
  }

  return strings.Count(string(raw), "\n")
}

func TestNewIDIsAUUIDv4(t *testing.T) {
  re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
  a, b := NewID(), NewID()
  if !re.MatchString(a) || a == b {
    t.Fatalf("ids %q %q", a, b)
  }
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
  withDataDir(t)
  m := newMeta()
  m.Title = "Fix off-by-one in Mean"

  err := Save(m, sampleHistory())
  if err != nil {
    t.Fatalf("Save: %v", err)
  }

  got, err := Load(m.ID)
  if err != nil {
    t.Fatalf("Load: %v", err)
  }

  if got.ID != m.ID || got.Root != m.Root || got.Title != m.Title || got.Model != "local" {
    t.Fatalf("meta = %+v", got.Meta)
  }
  if len(got.History) != 3 || got.History[2].Content != "hello" || got.Messages != 3 {
    t.Fatalf("history = %+v", got.History)
  }
  if got.CreatedAt.IsZero() || time.Since(got.UpdatedAt) > time.Minute {
    t.Fatalf("timestamps = %v / %v", got.CreatedAt, got.UpdatedAt)
  }
}

func TestLoadByUniquePrefixAndAmbiguity(t *testing.T) {
  withDataDir(t)
  a := &Meta{ ID: "aaaa1111-0000-4000-8000-000000000001" }
  b := &Meta{ ID: "aaaa2222-0000-4000-8000-000000000002" }
  Save(a, sampleHistory())
  Save(b, sampleHistory())

  got, err := Load("aaaa1")
  if err != nil || got.ID != a.ID {
    t.Fatalf("Load(aaaa1) = %v, %v", got, err)
  }

  _, err = Load("aaaa")
  if err == nil || !strings.Contains(err.Error(), "matches 2 sessions") {
    t.Fatalf("ambiguous prefix err = %v", err)
  }
  _, err = Load("zzzz")
  if err == nil || !strings.Contains(err.Error(), "no session") {
    t.Fatalf("missing err = %v", err)
  }
  _, err = Load("../etc/passwd")
  if err == nil {
    t.Fatal("a path-like id was accepted")
  }
}

func TestListNewestFirstWithCounts(t *testing.T) {
  withDataDir(t)
  old := newMeta()
  Save(old, sampleHistory())
  time.Sleep(10 * time.Millisecond)

  recent := newMeta()
  recent.Title = "newer"
  Save(recent, append(sampleHistory(), llm.Message{ Role: "user", Content: "more" }))

  list, err := List()
  if err != nil || len(list) != 2 {
    t.Fatalf("List = %v, %v", list, err)
  }
  if list[0].ID != recent.ID || list[0].Messages != 4 || list[1].Messages != 3 {
    t.Fatalf("order/counts = %+v", list)
  }
}

func TestLegacyPerProjectFileStillLists(t *testing.T) {
  withDataDir(t)
  dir, _ := Dir()
  os.MkdirAll(dir, 0o755)

  legacy := filepath.Join(dir, "fox-apsp-mpi-1a2b3c4d.jsonl")
  head, _ := json.Marshal(map[string]any{
    "root": "/home/u/fox-apsp-mpi", "model": "local", "updated_at": time.Now(),
  })
  body, _ := json.Marshal(llm.Message{ Role: "user", Content: "hi" })
  os.WriteFile(legacy, []byte(string(head) + "\n" + string(body) + "\n"), 0o644)

  list, _ := List()
  if len(list) != 1 || list[0].ID != "fox-apsp-mpi-1a2b3c4d" || list[0].Root != "/home/u/fox-apsp-mpi" {
    t.Fatalf("legacy listing = %+v", list)
  }

  got, err := Load("fox-apsp")
  if err != nil || len(got.History) != 1 {
    t.Fatalf("legacy Load = %v, %v", got, err)
  }

  latest, err := Latest("/home/u/fox-apsp-mpi")
  if err != nil || latest == nil || latest.ID != "fox-apsp-mpi-1a2b3c4d" {
    t.Fatalf("Latest = %v, %v", latest, err)
  }
}

func TestAppendFirstCallDoesFullSave(t *testing.T) {
  withDataDir(t)
  m := newMeta()
  persisted := 0

  err := Append(m, sampleHistory(), &persisted)
  if err != nil {
    t.Fatalf("Append: %v", err)
  }
  if persisted != 3 || lineCount(t, m.ID) != 4 {
    t.Fatalf("persisted = %d, lines = %d; want 3 and meta+3", persisted, lineCount(t, m.ID))
  }
}

func TestAppendOnlyWritesNewMessages(t *testing.T) {
  withDataDir(t)
  m := newMeta()
  h := sampleHistory()
  persisted := 0
  Append(m, h, &persisted)

  h = append(h, llm.Message{ Role: "user", Content: "next" })
  err := Append(m, h, &persisted)
  if err != nil {
    t.Fatalf("Append: %v", err)
  }
  if persisted != 4 || lineCount(t, m.ID) != 5 {
    t.Fatalf("persisted = %d, lines = %d", persisted, lineCount(t, m.ID))
  }

  err = Append(m, h, &persisted)
  if err != nil || lineCount(t, m.ID) != 5 {
    t.Fatal("an Append with nothing new wrote something")
  }
}

// TestAppendCostIsIndependentOfHistorySize is the O(n²)-session
// regression check: appending one message onto a long history must
// cost the same as onto a short one.
func TestAppendCostIsIndependentOfHistorySize(t *testing.T) {
  withDataDir(t)
  m := newMeta()

  history := []llm.Message{ { Role: "system", Content: "sys" } }
  persisted := 0
  err := Append(m, history, &persisted)
  if err != nil {
    t.Fatalf("seed Append: %v", err)
  }

  const seedMessages = 5000
  for i := 0; i < seedMessages; i++ {
    history = append(history, llm.Message{ Role: "user", Content: "filler message" })
  }
  err = Append(m, history, &persisted)
  if err != nil {
    t.Fatalf("bulk Append: %v", err)
  }

  const perAppendAllocLimit = 64
  history = append(history, llm.Message{ Role: "assistant", Content: "one more" })

  // A fresh copy of persisted each run, so every run appends exactly
  // one new message instead of later runs becoming no-ops.
  allocs := testing.AllocsPerRun(20, func() {
    p := persisted
    err := Append(m, history, &p)
    if err != nil {
      t.Fatalf("timed Append: %v", err)
    }
  })

  if allocs > perAppendAllocLimit {
    t.Fatalf(
      "Append allocated %.0f times to append 1 message onto a %d-message history; "+
        "want at most %d, independent of history length",
      allocs, len(history), perAppendAllocLimit,
    )
  }
}

func TestSaveOverwritesPreviousContent(t *testing.T) {
  withDataDir(t)
  m := newMeta()
  Save(m, sampleHistory())
  Save(m, sampleHistory()[:1])

  if lineCount(t, m.ID) != 2 {
    t.Fatalf("lines = %d, want meta+1 after a shorter Save", lineCount(t, m.ID))
  }
}

func TestProjectDataDirIsStable(t *testing.T) {
  withDataDir(t)
  a, _ := ProjectDataDir("memory", "/tmp/project")
  b, _ := ProjectDataDir("memory", "/tmp/project")
  if a != b || !strings.Contains(a, "project-") {
    t.Fatalf("ProjectDataDir = %q / %q", a, b)
  }
}

func TestDelete(t *testing.T) {
  withDataDir(t)
  m := newMeta()
  m.Title = "to delete"
  Save(m, sampleHistory())

  got, err := Delete(m.ID[:8])
  if err != nil || got.ID != m.ID || got.Title != "to delete" {
    t.Fatalf("Delete = %+v, %v", got, err)
  }
  _, err = Load(m.ID)
  if err == nil {
    t.Fatal("the session is still loadable after Delete")
  }
  _, err = Delete(m.ID)
  if err == nil {
    t.Fatal("deleting twice should fail")
  }
}
