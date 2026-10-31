package metrics

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestFlushMergesAndReports(t *testing.T) {
  d := t.TempDir()
  SetDir(d)
  defer SetDir("")
  Reset()

  Add("recall.turns", 4)
  Add("recall.attached", 2)
  Add("recall.used", 1)
  Observe("tool.read_file.ms", 3)
  Observe("tool.read_file.ms", 5)
  err := Flush()
  if err != nil {
    t.Fatal(err)
  }

  // a second process adds to the same file
  Add("recall.turns", 6)
  Observe("tool.read_file.ms", 1)
  err = Flush()
  if err != nil {
    t.Fatal(err)
  }

  all := read(filepath.Join(d, "metrics.tsv"))
  ms := all["tool.read_file.ms"]
  if all["recall.turns"].Count != 10 || ms.Count != 3 || ms.Sum != 9 ||
    ms.Min != 1 || ms.Max != 5 {
    t.Fatalf("merged: %+v %+v", all["recall.turns"], ms)
  }

  var b strings.Builder
  Report(&b, d)
  out := b.String()
  if !strings.Contains(out, "recall attached, of turns") ||
    !strings.Contains(out, "20.0%  (2 of 10)") ||
    !strings.Contains(out, "recall used by the model, of attached") {
    t.Fatalf("report:\n%s", out)
  }
}

func TestEventOnlyInDebug(t *testing.T) {
  d := t.TempDir()
  SetDir(d)
  defer SetDir("")

  Event("recall.attach", "nodes", 3)
  files, _ := os.ReadDir(d)
  if Debug != (len(files) == 1) {
    t.Fatalf("debug %v, %d event files", Debug, len(files))
  }
}
