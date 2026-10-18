package tools

import (
  "context"
  "testing"

  "armin/internal/sandbox"
)

// --- count_lines / list_languages ---

func TestCountLinesTotals(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "line1\nline2\nline3\n")
  writeFile(t, cfg.Root, "sub/b.go", "x\ny\n")

  out, err := CountLines(context.Background(), cfg, CountLinesInput{})
  if err != nil {
    t.Fatalf("CountLines: %v", err)
  }
  if out.TotalFiles != 2 {
    t.Fatalf("TotalFiles = %d, want 2", out.TotalFiles)
  }
  if out.TotalLines != 5 {
    t.Fatalf("TotalLines = %d, want 5", out.TotalLines)
  }
}

func TestCountLinesSkipsBinary(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "hello\n")
  writeFile(t, cfg.Root, "bin.dat", "\x00\x01\x02binary")

  out, err := CountLines(context.Background(), cfg, CountLinesInput{})
  if err != nil {
    t.Fatalf("CountLines: %v", err)
  }
  if out.TotalFiles != 1 {
    t.Fatalf("TotalFiles = %d, want 1 (binary file skipped)", out.TotalFiles)
  }
}

func TestListLanguagesGroupsByExtension(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "one\ntwo\n")
  writeFile(t, cfg.Root, "b.go", "one\n")
  writeFile(t, cfg.Root, "c.py", "one\ntwo\nthree\nfour\n")

  out, err := ListLanguages(context.Background(), cfg, ListLanguagesInput{})
  if err != nil {
    t.Fatalf("ListLanguages: %v", err)
  }

  byExt := map[string]LanguageCount{}
  for _, l := range out.Languages {
    byExt[l.Extension] = l
  }
  if byExt[".go"].Files != 2 || byExt[".go"].Lines != 3 {
    t.Fatalf(".go = %+v, want 2 files/3 lines", byExt[".go"])
  }
  if byExt[".py"].Files != 1 || byExt[".py"].Lines != 4 {
    t.Fatalf(".py = %+v, want 1 file/4 lines", byExt[".py"])
  }
  // Ranked by lines, most first: .py (4 lines) unambiguously outranks .go (3).
  if out.Languages[0].Extension != ".py" {
    t.Fatalf("Languages[0] = %q, want .py first", out.Languages[0].Extension)
  }
}

// --- find_files ---

func TestFindFilesMatchesGlob(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a_test.go", "")
  writeFile(t, cfg.Root, "a.go", "")
  writeFile(t, cfg.Root, "sub/b_test.go", "")

  out, err := FindFiles(context.Background(), cfg, FindFilesInput{ Pattern: "*_test.go" })
  if err != nil {
    t.Fatalf("FindFiles: %v", err)
  }
  if len(out.Files) != 2 {
    t.Fatalf("Files = %v, want 2 matches", out.Files)
  }
}

func TestFindFilesRequiresPattern(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := FindFiles(context.Background(), cfg, FindFilesInput{})
  if err == nil {
    t.Fatal("expected an error for an empty pattern")
  }
}

// --- file_stat ---

func TestFileStatReportsSizeAndLines(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "one\ntwo\nthree")

  out, err := FileStat(context.Background(), cfg, FileStatInput{ Path: "a.txt" })
  if err != nil {
    t.Fatalf("FileStat: %v", err)
  }
  if out.Lines != 3 || out.IsBinary {
    t.Fatalf("FileStat = %+v, want 3 lines, not binary", out)
  }
  if out.SizeBytes != int64(len("one\ntwo\nthree")) {
    t.Fatalf("SizeBytes = %d, want %d", out.SizeBytes, len("one\ntwo\nthree"))
  }
}

func TestFileStatRejectsDirectory(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "sub/a.txt", "x")
  _, err := FileStat(context.Background(), cfg, FileStatInput{ Path: "sub" })
  if err == nil {
    t.Fatal("expected an error for a directory")
  }
}

// --- find_todo_fixme ---

func TestFindTodoFixmeFindsMarkers(t *testing.T) {
  requireRipgrep(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.go", "// TODO: fix this\nfunc f() {}\n")

  out, err := FindTodoFixme(context.Background(), cfg, FindTodoFixmeInput{})
  if err != nil {
    t.Fatalf("FindTodoFixme: %v", err)
  }
  if out.Matches == "(no matches)" {
    t.Fatal("expected the TODO marker to be found")
  }
}

// --- env_info ---

func TestEnvInfoReportsOSAndArch(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  out, err := EnvInfo(context.Background(), cfg, EnvInfoInput{})
  if err != nil {
    t.Fatalf("EnvInfo: %v", err)
  }
  if out.OS == "" || out.Arch == "" {
    t.Fatalf("EnvInfo = %+v, want non-empty OS/Arch", out)
  }
}

// --- project_manifest ---

func TestProjectManifestIncludesReadmeAndTree(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "README.md", "# My Project\nDoes things.\n")
  writeFile(t, cfg.Root, "main.go", "package main\n")

  out, err := ProjectManifest(context.Background(), cfg, ProjectManifestInput{})
  if err != nil {
    t.Fatalf("ProjectManifest: %v", err)
  }
  if out.Note == "" {
    t.Fatal("expected a non-empty manifest note")
  }
}
