package install

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

const testConf = `model = local

models.local.binary     = llama-server
models.local.model_path = /m/a.gguf

# org/big:Q8_0, added by armin -install on 2026-10-21
models.big.binary     = llama-server
models.big.model_path = /m/big-00001-of-00002.gguf
models.big.port       = 8105

models.hosted.url = https://example.com/v1
`

func TestWithoutModel(t *testing.T) {
  kept, removed := withoutModel(testConf, "big")
  if strings.Contains(kept, "big") || strings.Contains(kept, "added by armin") {
    t.Errorf("left behind:\n%s", kept)
  }

  if len(removed) != 4 {
    t.Errorf("removed %d lines: %q", len(removed), removed)
  }

  // One blank line between the neighbours, not two.
  if !strings.Contains(kept, "a.gguf\n\nmodels.hosted") {
    t.Errorf("spacing:\n%s", kept)
  }

  // A name that's a prefix of another leaves the other alone.
  kept, _ = withoutModel("models.big2.port = 1\nmodels.big.port = 2\n", "big")
  if kept != "models.big2.port = 1\n" {
    t.Errorf("prefix: %q", kept)
  }
}

func TestModelFiles(t *testing.T) {
  dir := t.TempDir()
  for _, f := range []string{
    "big-00001-of-00002.gguf", "big-00002-of-00002.gguf",
    "big-00002-of-00002.gguf.part", "other.gguf",
  } {
    os.WriteFile(filepath.Join(dir, f), []byte("12345"), 0o644)
  }

  big := Existing{ ModelPath: filepath.Join(dir, "big-00001-of-00002.gguf") }
  files, shared := modelFiles("big", big, map[string]Existing{ "big": big })
  if len(files) != 3 || len(shared) != 0 {
    t.Errorf("split: files %+v shared %v", files, shared)
  }

  // Another model on the same file keeps it.
  twin := Existing{ ModelPath: big.ModelPath }
  files, shared = modelFiles("big", big, map[string]Existing{ "big": big, "twin": twin })
  if len(files) != 0 || len(shared) != 2 {
    t.Errorf("shared: files %+v shared %v", files, shared)
  }

  // Remote and hosted models delete nothing.
  files, _ = modelFiles("r", Existing{ ModelPath: big.ModelPath, Remote: true }, nil)
  if len(files) != 0 {
    t.Errorf("remote: %+v", files)
  }
}

func TestUninstall(t *testing.T) {
  home := t.TempDir()
  t.Setenv("XDG_CONFIG_HOME", home)
  dir := filepath.Join(home, "armin")
  os.MkdirAll(dir, 0o755)

  model := filepath.Join(home, "big.gguf")
  os.WriteFile(model, []byte("weights"), 0o644)
  conf := strings.ReplaceAll(testConf, "/m/big-00001-of-00002.gguf", model)
  path := filepath.Join(dir, "config.conf")
  os.WriteFile(path, []byte(conf), 0o600)

  models := map[string]Existing{
    "local":  { ModelPath: "/m/a.gguf" },
    "big":    { ModelPath: model },
    "hosted": { External: true },
  }

  err := Uninstall(Request{ Model: "local", Yes: true, Models: models, Default: "local" })
  if err == nil || !strings.Contains(err.Error(), "model =") {
    t.Errorf("uninstalling the default: %v", err)
  }

  err = Uninstall(Request{ Model: "nope", Yes: true, Models: models })
  if err == nil {
    t.Error("unknown model: no error")
  }

  err = Uninstall(Request{ Model: "big", Yes: true, Models: models, Default: "local" })
  if err != nil {
    t.Fatal(err)
  }

  got, _ := os.ReadFile(path)
  if strings.Contains(string(got), "models.big") || !strings.Contains(string(got), "models.hosted") {
    t.Errorf("config after:\n%s", got)
  }

  info, _ := os.Stat(path)
  if info.Mode().Perm() != 0o600 {
    t.Errorf("mode %v", info.Mode().Perm())
  }

  _, err = os.Stat(model)
  if !os.IsNotExist(err) {
    t.Error("model file kept")
  }
}
