package notes

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestSafeName(t *testing.T) {
  cases := map[string]string{
    "Build Steps":  "build-steps",
    "../etc/passwd": "etc-passwd",
    "plan.md":      "plan",
    "a_b-c":        "a_b-c",
  }

  for in, want := range cases {
    got, err := SafeName(in)
    if err != nil || got != want {
      t.Errorf("SafeName(%q) = %q, %v; want %q", in, got, err, want)
    }
  }

  _, err := SafeName("///")
  if err == nil {
    t.Error("SafeName(\"///\") should fail")
  }
}

func TestMemoriesRoundTripAndScopes(t *testing.T) {
  m := &Memories{
    ProjectDir: filepath.Join(t.TempDir(), "p"),
    UserDir:    filepath.Join(t.TempDir(), "u"),
  }

  _, err := m.Save("style", "", "x", ScopeUser)
  if err == nil {
    t.Fatal("expected a missing description to be rejected")
  }

  _, err = m.Save("Style", "2-space\nindent", "use 2 spaces", ScopeUser)
  if err != nil {
    t.Fatal(err)
  }

  _, err = m.Save("db", "mysql 8", "not postgres", ScopeProject)
  if err != nil {
    t.Fatal(err)
  }

  mem, body, err := m.Read("style")
  if err != nil {
    t.Fatal(err)
  }

  if mem.Scope != ScopeUser || mem.Description != "2-space indent" || body != "\nuse 2 spaces\n" {
    t.Fatalf("Read = %+v %q", mem, body)
  }

  index := m.Index()
  if !strings.Contains(index, "- db (project): mysql 8") || !strings.Contains(index, "- style (user)") {
    t.Fatalf("Index =\n%s", index)
  }

  // Re-saving in the other scope moves it rather than duplicating.
  _, err = m.Save("style", "moved", "y", ScopeProject)
  if err != nil {
    t.Fatal(err)
  }

  list, _ := m.List()
  if len(list) != 2 {
    t.Fatalf("List = %+v, want 2 entries after the move", list)
  }

  _, err = m.Delete("db")
  if err != nil {
    t.Fatal(err)
  }

  _, _, err = m.Read("db")
  if err == nil {
    t.Fatal("db still readable after Delete")
  }
}

func TestSkillsLoadOverrideAndJail(t *testing.T) {
  user, project := t.TempDir(), t.TempDir()
  write := func(dir, rel, content string) {
    p := filepath.Join(dir, rel)
    os.MkdirAll(filepath.Dir(p), 0o755)
    os.WriteFile(p, []byte(content), 0o644)
  }

  write(user, "review/SKILL.md", "---\nname: review\ndescription: user version\n---\nuser body\n")
  write(user, "deploy/SKILL.md", "---\nname: deploy\ndescription: ship it\n---\nsteps\n")
  write(project, "review/SKILL.md", "---\nname: review\ndescription: project version\n---\nproject body\n")
  write(project, "review/checklist.md", "- [ ] tests\n")
  write(project, "secret.txt", "nope")

  s := LoadSkills(user, project)
  if s.Len() != 2 {
    t.Fatalf("Len = %d, want 2", s.Len())
  }

  sk, ok := s.Get("review")
  if !ok || !sk.Project || sk.Description != "project version" {
    t.Fatalf("review = %+v, want the project skill", sk)
  }

  body, _ := sk.Instructions()
  if body != "project body\n" {
    t.Fatalf("Instructions = %q", body)
  }

  files := sk.Files()
  if len(files) != 1 || files[0] != "checklist.md" {
    t.Fatalf("Files = %v", files)
  }

  _, _, err := sk.ReadFile("../secret.txt")
  if err == nil {
    t.Fatal("ReadFile escaped the skill directory")
  }
}

func TestScratchpadWriteAppendRead(t *testing.T) {
  p := &Scratchpad{ Dir: t.TempDir() }

  _, err := p.Write("plan", "1. read\n", false)
  if err != nil {
    t.Fatal(err)
  }

  f, err := p.Write("plan", "2. edit", true)
  if err != nil {
    t.Fatal(err)
  }

  if f.Lines != 2 {
    t.Fatalf("Lines = %d, want 2", f.Lines)
  }

  got, total, err := p.Read("plan", 2, 0)
  if err != nil || got != "2. edit" || total != 2 {
    t.Fatalf("Read = %q, %d, %v", got, total, err)
  }

  if !strings.Contains(p.Summary(), "- plan (2 lines)") {
    t.Fatalf("Summary = %q", p.Summary())
  }

  n, _ := p.Clear()
  if n != 1 {
    t.Fatalf("Clear removed %d, want 1", n)
  }
}

func TestProjectInstructions(t *testing.T) {
  root := t.TempDir()
  os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("run make"), 0o644)
  name, body := ProjectInstructions(root)
  if name != "AGENTS.md" || body != "run make" {
    t.Fatalf("= %q, %q", name, body)
  }
}

func TestScratchpadArchiveRotatesAndPointsAtLines(t *testing.T) {
  p := &Scratchpad{ Dir: t.TempDir() }

  name, start, end, err := p.Archive("read_file path=a.go", "line1\nline2\n")
  if err != nil || name != "archive-1" || start != 1 || end != 3 {
    t.Fatalf("first = %s %d-%d %v", name, start, end, err)
  }

  got, _, _ := p.Read(name, start, end)
  if !strings.Contains(got, "read_file path=a.go") || !strings.Contains(got, "line2") {
    t.Fatalf("read back %q", got)
  }

  _, start2, _, _ := p.Archive("second", "x")
  if start2 <= end {
    t.Fatalf("second entry starts at %d, want after %d", start2, end)
  }

  big := strings.Repeat("y", maxScratchBytes - 30)
  name3, _, _, err := p.Archive("big", big)
  if err != nil || name3 != "archive-2" {
    t.Fatalf("big entry went to %s (%v), want rotation to archive-2", name3, err)
  }
}
