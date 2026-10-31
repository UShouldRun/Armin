package agent

import (
  "context"
  "encoding/json"
  "flag"
  "os"
  "path/filepath"
  "testing"

  "armin/internal/know"
  "armin/internal/library"
  "armin/internal/notes"
)

var update = flag.Bool("update", false, "rewrite the testdata/definitions*.json files")

// TestDefinitionsGolden pins what the model is told about its tools,
// byte for byte: an accidental change to a description or a schema
// changes model behavior and throws away every prompt cache. Change
// it on purpose with: go test ./internal/agent -run Golden -update
func TestDefinitionsGolden(t *testing.T) {
  dir := t.TempDir()
  os.MkdirAll(filepath.Join(dir, "skills", "demo"), 0o755)
  os.WriteFile(filepath.Join(dir, "skills", "demo", "SKILL.md"),
    []byte("---\nname: demo\ndescription: a demo skill\n---\nDo it.\n"), 0o644)
  os.MkdirAll(filepath.Join(dir, "lib", "go"), 0o755)
  lib, err := library.Open(filepath.Join(dir, "lib"))
  if err != nil {
    t.Fatal(err)
  }

  lib.Build(context.Background(), nil)
  l := &Loop{
    Memories: &notes.Memories{},
    Scratch:  &notes.Scratchpad{},
    Skills:   notes.LoadSkills(filepath.Join(dir, "skills"), ""),
    Library:  lib,
    Know:     know.New(),
  }

  all, _ := json.MarshalIndent(l.Definitions(), "", " ")
  bare, _ := json.MarshalIndent(ToolDefinitions(), "", " ")
  got := append(append(all, '\n'), bare...)

  checkGolden(t, "definitions.json", got)

  // one file per role: each role's list must stay byte-stable too,
  // or switching roles throws the prompt cache away
  for _, r := range []Role{ RoleMinion, RoleCopilot, RoleAdvisor } {
    l.SetRole(r)
    defs, _ := json.MarshalIndent(l.Definitions(), "", " ")
    checkGolden(t, "definitions." + r.String() + ".json", defs)
  }
}

func checkGolden(t *testing.T, name string, got []byte) {
  t.Helper()
  golden := filepath.Join("testdata", name)
  if *update {
    os.MkdirAll("testdata", 0o755)
    os.WriteFile(golden, got, 0o644)
  }

  want, err := os.ReadFile(golden)
  if err != nil {
    t.Fatalf("%v (create it with -update)", err)
  }

  if string(got) != string(want) {
    os.WriteFile(golden + ".got", got, 0o644)
    t.Fatalf("tool definitions changed; diff testdata/%s{,.got}", name)
  }
}
