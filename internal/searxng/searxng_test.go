package searxng

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

func TestTruncateSnippetShortStringUnchanged(t *testing.T) {
  s := "a short snippet"
  if got := truncateSnippet(s); got != s {
    t.Fatalf("truncateSnippet(%q) = %q, want unchanged", s, got)
  }
}

func TestTruncateSnippetLongStringCut(t *testing.T) {
  s := strings.Repeat("x", maxSnippetChars+50)
  got := truncateSnippet(s)
  if len(got) > maxSnippetChars+len("…") {
    t.Fatalf("truncateSnippet result is %d bytes, want at most %d",
      len(got), maxSnippetChars+len("…"))
  }
  if !strings.HasSuffix(got, "…") {
    t.Fatalf("truncateSnippet(%d chars) = %q, want a truncation marker", len(s), got)
  }
}

func TestToResultsCapsAtRequestedCount(t *testing.T) {
  var parsed searxResponse
  for i := 0; i < 8; i++ {
    parsed.Results = append(parsed.Results, struct {
      URL     string `json:"url"`
      Title   string `json:"title"`
      Content string `json:"content"`
    }{ URL: "u", Title: "t", Content: "c" })
  }

  got := toResults(parsed, 3)
  if len(got) != 3 {
    t.Fatalf("toResults returned %d results, want 3", len(got))
  }
}

func TestToResultsCountAboveAvailableIsClamped(t *testing.T) {
  var parsed searxResponse
  parsed.Results = append(parsed.Results, struct {
    URL     string `json:"url"`
    Title   string `json:"title"`
    Content string `json:"content"`
  }{ URL: "u", Title: "t", Content: "c" })

  got := toResults(parsed, 10)
  if len(got) != 1 {
    t.Fatalf("toResults returned %d results, want 1 (all that's available)", len(got))
  }
}

func TestEnsureSettingsWritesFileOnce(t *testing.T) {
  t.Setenv("XDG_CONFIG_HOME", t.TempDir())

  path1, err := ensureSettings()
  if err != nil {
    t.Fatalf("ensureSettings: %v", err)
  }
  content1, err := os.ReadFile(path1)
  if err != nil {
    t.Fatal(err)
  }
  if !strings.Contains(string(content1), "formats") {
    t.Fatalf("settings.yml missing the json format override: %q", content1)
  }

  path2, err := ensureSettings()
  if err != nil {
    t.Fatalf("ensureSettings (second call): %v", err)
  }
  content2, err := os.ReadFile(path2)
  if err != nil {
    t.Fatal(err)
  }
  if string(content1) != string(content2) {
    t.Fatal("ensureSettings regenerated the file (and its secret_key) on a second call")
  }
}

func TestEnsureSettingsPathUnderConfigDir(t *testing.T) {
  home := t.TempDir()
  t.Setenv("XDG_CONFIG_HOME", home)

  path, err := ensureSettings()
  if err != nil {
    t.Fatalf("ensureSettings: %v", err)
  }
  want := filepath.Join(home, "armin", "searxng", "settings.yml")
  if path != want {
    t.Fatalf("ensureSettings path = %q, want %q", path, want)
  }
}
