package searxng

import (
  "encoding/json"
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
  s := strings.Repeat("x", maxSnippetChars + 50)
  got := truncateSnippet(s)
  if len(got) > maxSnippetChars + len("…") {
    t.Fatalf("truncateSnippet result is %d bytes, want at most %d",
      len(got), maxSnippetChars + len("…"))
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

  got := toResults(parsed, "", 3)
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

  got := toResults(parsed, "", 10)
  if len(got) != 1 {
    t.Fatalf("toResults returned %d results, want 1 (all that's available)", len(got))
  }
}

func TestEnsureSettingsWritesFileOnce(t *testing.T) {
  t.Setenv("XDG_CONFIG_HOME", t.TempDir())

  path1, changed, err := ensureSettings()
  if err != nil {
    t.Fatalf("ensureSettings: %v", err)
  }
  content1, err := os.ReadFile(path1)
  if err != nil {
    t.Fatal(err)
  }
  if !changed {
    t.Error("first write should report a change")
  }
  if !strings.Contains(string(content1), "formats") {
    t.Fatalf("settings.yml missing the json format override: %q", content1)
  }

  path2, changed, err := ensureSettings()
  if err != nil {
    t.Fatalf("ensureSettings (second call): %v", err)
  }
  content2, err := os.ReadFile(path2)
  if err != nil {
    t.Fatal(err)
  }
  if changed || string(content1) != string(content2) {
    t.Fatal("ensureSettings regenerated the file (and its secret_key) on a second call")
  }
}

func TestEnsureSettingsPathUnderConfigDir(t *testing.T) {
  home := t.TempDir()
  t.Setenv("XDG_CONFIG_HOME", home)

  path, _, err := ensureSettings()
  if err != nil {
    t.Fatalf("ensureSettings: %v", err)
  }
  want := filepath.Join(home, "armin", "searxng", "settings.yml")
  if path != want {
    t.Fatalf("ensureSettings path = %q, want %q", path, want)
  }
}

func TestEnsureSettingsUpgradesOldFileKeepingSecret(t *testing.T) {
  home := t.TempDir()
  t.Setenv("XDG_CONFIG_HOME", home)
  dir := filepath.Join(home, "armin", "searxng")
  os.MkdirAll(dir, 0o755)
  old := "use_default_settings: true\n\nserver:\n  secret_key: \"abc123\"\n\nsearch:\n  formats:\n    - html\n    - json\n"
  os.WriteFile(filepath.Join(dir, "settings.yml"), []byte(old), 0o644)

  path, changed, err := ensureSettings()
  if err != nil || !changed {
    t.Fatalf("changed=%v err=%v, want the old template rewritten", changed, err)
  }
  got, _ := os.ReadFile(path)
  for _, want := range []string{ "abc123", "keep_only", "- yandex", "limiter: false" } {
    if !strings.Contains(string(got), want) {
      t.Errorf("rewritten settings missing %q:\n%s", want, got)
    }
  }
  if strings.Contains(string(got), "duckduckgo") {
    t.Error("the CAPTCHA-blocked engines are back")
  }
}

func TestEnsureSettingsLeavesUserFileAlone(t *testing.T) {
  home := t.TempDir()
  t.Setenv("XDG_CONFIG_HOME", home)
  dir := filepath.Join(home, "armin", "searxng")
  os.MkdirAll(dir, 0o755)
  mine := "# my own config\nuse_default_settings: true\nserver:\n  secret_key: \"x\"\n"
  os.WriteFile(filepath.Join(dir, "settings.yml"), []byte(mine), 0o644)

  path, changed, _ := ensureSettings()
  got, _ := os.ReadFile(path)
  if changed || string(got) != mine {
    t.Fatal("a hand-written settings.yml was overwritten")
  }
}

func TestCategory(t *testing.T) {
  cases := map[string]string{ "": "general", "general": "general", "code": "general,it", "science": "science,general", "bogus": "general" }
  for in, want := range cases {
    if got := Category(in); got != want {
      t.Errorf("Category(%q) = %q, want %q", in, got, want)
    }
  }
}

func TestToResultsDropsResultsUnrelatedToTheQuery(t *testing.T) {
  var parsed searxResponse
  json.Unmarshal([]byte(`{"results":[
    {"title":"Lidl Portugal","url":"https://lidl.pt","content":"Promoções da semana"},
    {"title":"MPI_Cart_create","url":"https://mpich.org/MPI_Cart_create.html","content":"Makes a new communicator"},
    {"title":"Cartesian topologies","url":"https://example.edu/mpi","content":"MPI_CART_SHIFT returns neighbors"}
  ]}`), &parsed)

  got := toResults(parsed, "MPI_Cart_create MPI_Cart_shift docs", 10)
  if len(got) != 2 || got[0].URL != "https://mpich.org/MPI_Cart_create.html" {
    t.Fatalf("results = %+v, want the two MPI pages only", got)
  }
}
