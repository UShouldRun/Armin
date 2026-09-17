package session

import (
  "os"
  "path/filepath"
  "testing"
  "time"

  "armin/internal/llm"
)

// withSessionDir points Dir() at a temp directory for the duration of
// the test, via XDG_DATA_HOME, so tests never touch the real
// ~/.local/share/armin/sessions.
func withSessionDir(t *testing.T) string {
  t.Helper()
  dir := t.TempDir()
  t.Setenv("XDG_DATA_HOME", dir)
  return dir
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  history := []llm.Message{
    {Role: "system", Content: "sys"},
    {Role: "user", Content: "hi"},
    {Role: "assistant", Content: "hello"},
  }

  if err := Save(root, "qwen3:4b", history); err != nil {
    t.Fatalf("Save: %v", err)
  }

  saved, err := Load(root)
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  if saved == nil {
    t.Fatal("Load returned nil, want a saved session")
  }
  if saved.Model != "qwen3:4b" || saved.Root != root {
    t.Fatalf("Load() = %+v, want model qwen3:4b root %s", saved, root)
  }
  if len(saved.History) != len(history) {
    t.Fatalf("Load() history has %d messages, want %d", len(saved.History), len(history))
  }
  for i, m := range saved.History {
    if m.Role != history[i].Role || m.Content != history[i].Content {
      t.Fatalf("message %d = %+v, want %+v", i, m, history[i])
    }
  }
}

func TestLoadMissingSessionReturnsNilNil(t *testing.T) {
  withSessionDir(t)

  saved, err := Load(t.TempDir())
  if err != nil {
    t.Fatalf("Load on a missing session returned an error: %v", err)
  }
  if saved != nil {
    t.Fatalf("Load on a missing session = %+v, want nil", saved)
  }
}

func TestAppendFirstCallDoesFullSave(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  history := []llm.Message{{Role: "system", Content: "sys"}}
  persisted := 0

  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("Append: %v", err)
  }
  if persisted != 1 {
    t.Fatalf("persisted = %d, want 1", persisted)
  }

  saved, err := Load(root)
  if err != nil || saved == nil {
    t.Fatalf("Load after first Append: saved=%+v err=%v", saved, err)
  }
  if len(saved.History) != 1 {
    t.Fatalf("history has %d messages, want 1", len(saved.History))
  }
}

func TestAppendOnlyWritesNewMessages(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  history := []llm.Message{{Role: "system", Content: "sys"}}
  persisted := 0
  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("initial Append: %v", err)
  }

  history = append(history, llm.Message{Role: "user", Content: "one"})
  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("second Append: %v", err)
  }
  if persisted != 2 {
    t.Fatalf("persisted = %d, want 2", persisted)
  }

  history = append(history, llm.Message{Role: "assistant", Content: "two"})
  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("third Append: %v", err)
  }

  saved, err := Load(root)
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  if len(saved.History) != 3 {
    t.Fatalf("history has %d messages, want 3", len(saved.History))
  }
  if saved.History[2].Content != "two" {
    t.Fatalf("history[2] = %+v, want content \"two\"", saved.History[2])
  }
}

func TestAppendWithNothingNewIsANoOp(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  history := []llm.Message{{Role: "system", Content: "sys"}}
  persisted := len(history) // pretend everything is already on disk

  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("Append: %v", err)
  }

  // Nothing was ever actually written — Load should see no session.
  saved, err := Load(root)
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  if saved != nil {
    t.Fatalf("Load() = %+v, want nil (Append had nothing new to write)", saved)
  }
}

// TestAppendCostIsIndependentOfHistorySize is the regression test for
// the exact bug this package was rewritten to fix: a REPL that
// autosaves after every turn by re-serializing and rewriting the whole
// history does O(n) work per turn and O(n^2) total I/O over an
// n-message session. Append should do a bounded amount of work per
// call regardless of how long the history already is — this asserts
// that directly instead of trusting the implementation by inspection.
func TestAppendCostIsIndependentOfHistorySize(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  history := []llm.Message{{Role: "system", Content: "sys"}}
  persisted := 0
  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("seed Append: %v", err)
  }

  // Grow history to a few thousand messages first, as if a long
  // session had already happened, without exercising Append's cost
  // for each one (this loop's own cost isn't what's under test).
  const seedMessages = 5000
  for i := 0; i < seedMessages; i++ {
    history = append(history, llm.Message{Role: "user", Content: "filler message"})
  }
  if err := Append(root, "m", history, &persisted); err != nil {
    t.Fatalf("bulk Append: %v", err)
  }

  // Now measure the cost of appending exactly one more message onto
  // an already-long history — this must not scale with len(history).
  const perAppendAllocLimit = 64
  history = append(history, llm.Message{Role: "assistant", Content: "one more"})

  // p is a fresh copy of persisted on every iteration, so each of the
  // 20 runs repeats the same "append exactly 1 new message" case
  // rather than measuring only the first call (every later call would
  // see p already caught up and become a strictly cheaper no-op).
  allocs := testing.AllocsPerRun(20, func() {
    p := persisted
    if err := Append(root, "m", history, &p); err != nil {
      t.Fatalf("timed Append: %v", err)
    }
  })

  if allocs > perAppendAllocLimit {
    t.Fatalf(
      "Append allocated %.0f times to append 1 message onto a %d-message history — "+
        "want at most %d, independent of history length (this is the O(n^2)-session regression check)",
      allocs, len(history), perAppendAllocLimit,
    )
  }
}

func TestPathForIsStableAndFilesystemSafe(t *testing.T) {
  withSessionDir(t)

  root := filepath.Join(t.TempDir(), "My Project!")
  if err := os.MkdirAll(root, 0o755); err != nil {
    t.Fatal(err)
  }

  p1, err := pathFor(root)
  if err != nil {
    t.Fatalf("pathFor: %v", err)
  }
  p2, err := pathFor(root)
  if err != nil {
    t.Fatalf("pathFor: %v", err)
  }
  if p1 != p2 {
    t.Fatalf("pathFor is not stable: %q != %q", p1, p2)
  }
  if filepath.Ext(p1) != ".jsonl" {
    t.Fatalf("pathFor = %q, want a .jsonl file", p1)
  }
}

func TestLoadRejectsCorruptMetadataLine(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  path, err := pathFor(root)
  if err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte("not json\n"), 0o644); err != nil {
    t.Fatal(err)
  }

  if _, err := Load(root); err == nil {
    t.Fatal("Load on a corrupt metadata line succeeded, want an error")
  }
}

func TestSaveOverwritesPreviousContent(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  long := make([]llm.Message, 10)
  for i := range long {
    long[i] = llm.Message{Role: "user", Content: "message"}
  }
  if err := Save(root, "m", long); err != nil {
    t.Fatal(err)
  }

  short := []llm.Message{{Role: "system", Content: "sys"}}
  if err := Save(root, "m", short); err != nil {
    t.Fatal(err)
  }

  saved, err := Load(root)
  if err != nil {
    t.Fatal(err)
  }
  if len(saved.History) != 1 {
    t.Fatalf("history has %d messages after overwrite, want 1 (Save must truncate, not append)", len(saved.History))
  }
}

func TestMetaUpdatedAtIsRecent(t *testing.T) {
  withSessionDir(t)
  root := t.TempDir()

  before := time.Now().Add(-time.Second)
  if err := Save(root, "m", []llm.Message{{Role: "system", Content: "s"}}); err != nil {
    t.Fatal(err)
  }
  after := time.Now().Add(time.Second)

  saved, err := Load(root)
  if err != nil {
    t.Fatal(err)
  }
  if saved.UpdatedAt.Before(before) || saved.UpdatedAt.After(after) {
    t.Fatalf("UpdatedAt = %v, want between %v and %v", saved.UpdatedAt, before, after)
  }
}
