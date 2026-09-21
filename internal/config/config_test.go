package config

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"
)

func withConfigDir(t *testing.T) string {
  t.Helper()
  dir := t.TempDir()
  t.Setenv("XDG_CONFIG_HOME", dir)
  return dir
}

func writeConfig(t *testing.T, contents string) {
  t.Helper()
  path, err := Path()
  if err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
    t.Fatal(err)
  }
}

func TestLoadMissingFileReturnsZeroValue(t *testing.T) {
  withConfigDir(t)

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load on a missing config file returned an error: %v", err)
  }
  if cfg.Model != "" || cfg.LlamaURL != "" || cfg.Timeout != 0 ||
    cfg.MaxOutput != 0 || cfg.ExtraAllowed != nil || cfg.Models != nil {
    t.Fatalf("Load() = %+v, want every field zero-valued", cfg)
  }
}

func TestLoadParsesEveryKey(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
# a comment, and a blank line above

model = qwen3-chat
completion_model = qwen2.5-coder
ctags_bin = /usr/local/bin/universal-ctags
llama_url = http://localhost:8080
completion_llama_url = http://localhost:8081
root = /some/project
timeout = 30s
max_output = 5000
allow = curl, docker
`)

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }

  want := Config{
    Model:              "qwen3-chat",
    CompletionModel:    "qwen2.5-coder",
    CtagsBinary:        "/usr/local/bin/universal-ctags",
    LlamaURL:           "http://localhost:8080",
    CompletionLlamaURL: "http://localhost:8081",
    Root:               "/some/project",
    Timeout:            30 * time.Second,
    MaxOutput:          5000,
    ExtraAllowed:       []string{"curl", "docker"},
  }

  if cfg.Model != want.Model || cfg.CompletionModel != want.CompletionModel ||
    cfg.CtagsBinary != want.CtagsBinary || cfg.LlamaURL != want.LlamaURL ||
    cfg.CompletionLlamaURL != want.CompletionLlamaURL ||
    cfg.Root != want.Root || cfg.Timeout != want.Timeout || cfg.MaxOutput != want.MaxOutput {
    t.Fatalf("Load() = %+v, want %+v", cfg, want)
  }
  if len(cfg.ExtraAllowed) != 2 || cfg.ExtraAllowed[0] != "curl" || cfg.ExtraAllowed[1] != "docker" {
    t.Fatalf("ExtraAllowed = %v, want [curl docker]", cfg.ExtraAllowed)
  }
}

func TestLoadParsesModelBlocks(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
models.chat.url         = http://localhost:8080
models.chat.api_key_env = ANTHROPIC_API_KEY

models.fim.binary = llama-server
models.fim.model_path = /home/h/models/qwen2.5-coder.gguf
models.fim.port = 8081
models.fim.idle_timeout = 5m
models.fim.args = --ctx-size 8192 --n-gpu-layers 999
`)

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }

  if len(cfg.Models) != 2 {
    t.Fatalf("Models = %+v, want 2 entries", cfg.Models)
  }

  chat := cfg.Models["chat"]
  if chat.URL != "http://localhost:8080" {
    t.Fatalf("chat.URL = %q", chat.URL)
  }
  if chat.APIKeyEnv != "ANTHROPIC_API_KEY" {
    t.Fatalf("chat.APIKeyEnv = %q, want ANTHROPIC_API_KEY", chat.APIKeyEnv)
  }

  fim := cfg.Models["fim"]
  if fim.Binary != "llama-server" || fim.ModelPath != "/home/h/models/qwen2.5-coder.gguf" {
    t.Fatalf("fim = %+v", fim)
  }
  if fim.Port != 8081 {
    t.Fatalf("fim.Port = %d, want 8081", fim.Port)
  }
  if fim.IdleTimeout != 5*time.Minute {
    t.Fatalf("fim.IdleTimeout = %v, want 5m", fim.IdleTimeout)
  }
  want := []string{"--ctx-size", "8192", "--n-gpu-layers", "999"}
  if len(fim.Args) != len(want) {
    t.Fatalf("fim.Args = %v, want %v", fim.Args, want)
  }
  for i := range want {
    if fim.Args[i] != want[i] {
      t.Fatalf("fim.Args = %v, want %v", fim.Args, want)
    }
  }
}

func TestLoadRejectsUnknownModelKey(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "models.fim.not_a_real_field = value\n")

  if _, err := Load(); err == nil {
    t.Fatal("expected an error for an unknown models.<name>.<field> key")
  }
}

func TestLoadRejectsBadModelPort(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "models.fim.port = not-a-number\n")

  if _, err := Load(); err == nil {
    t.Fatal("expected an error for an unparseable models.<name>.port")
  }
}

func TestLoadRejectsUnknownKey(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "not_a_real_key = value\n")

  if _, err := Load(); err == nil {
    t.Fatal("expected an error for an unknown config key")
  }
}

func TestLoadRejectsMalformedLine(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "this line has no equals sign\n")

  if _, err := Load(); err == nil {
    t.Fatal("expected an error for a line with no \"key = value\" shape")
  }
}

func TestLoadRejectsBadTimeout(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "timeout = not-a-duration\n")

  if _, err := Load(); err == nil {
    t.Fatal("expected an error for an unparseable timeout")
  }
}

func TestLoadRejectsBadMaxOutput(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "max_output = not-a-number\n")

  if _, err := Load(); err == nil {
    t.Fatal("expected an error for an unparseable max_output")
  }
}

func TestLoadIgnoresBlankLinesAndComments(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "\n# just a comment\n\nmodel = qwen3:4b\n\n# trailing comment\n")

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  if cfg.Model != "qwen3:4b" {
    t.Fatalf("Model = %q, want qwen3:4b", cfg.Model)
  }
}

func TestLoadEnvSetsUnsetVars(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte("FOO_KEY=abc123\n# a comment\n\nBAR_KEY = spaced \n"), 0o600); err != nil {
    t.Fatal(err)
  }

  os.Unsetenv("FOO_KEY")
  os.Unsetenv("BAR_KEY")
  t.Cleanup(func() { os.Unsetenv("FOO_KEY"); os.Unsetenv("BAR_KEY") })

  if err := LoadEnv(); err != nil {
    t.Fatalf("LoadEnv: %v", err)
  }

  if got := os.Getenv("FOO_KEY"); got != "abc123" {
    t.Fatalf("FOO_KEY = %q, want abc123", got)
  }
  if got := os.Getenv("BAR_KEY"); got != "spaced" {
    t.Fatalf("BAR_KEY = %q, want \"spaced\" (trimmed)", got)
  }
}

func TestLoadEnvDoesNotOverrideRealEnvironment(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte("PRESET_KEY=from-dotenv\n"), 0o600); err != nil {
    t.Fatal(err)
  }

  t.Setenv("PRESET_KEY", "from-real-env")

  if err := LoadEnv(); err != nil {
    t.Fatalf("LoadEnv: %v", err)
  }
  if got := os.Getenv("PRESET_KEY"); got != "from-real-env" {
    t.Fatalf("PRESET_KEY = %q, want the real environment to win over .env", got)
  }
}

func TestLoadEnvMissingFileIsNotError(t *testing.T) {
  withConfigDir(t)
  if err := LoadEnv(); err != nil {
    t.Fatalf("LoadEnv on a missing .env returned an error: %v", err)
  }
}

func TestLoadEnvRejectsMalformedLine(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte("not a key value line\n"), 0o600); err != nil {
    t.Fatal(err)
  }

  if err := LoadEnv(); err == nil {
    t.Fatal("expected an error for a line with no \"KEY=value\" shape")
  }
}

func TestLoadEnvRejectsLoosePermissions(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    t.Fatal(err)
  }
  if err := os.WriteFile(path, []byte("FOO_KEY=abc123\n"), 0o644); err != nil {
    t.Fatal(err)
  }

  err = LoadEnv()
  if err == nil {
    t.Fatal("expected an error for a group/other-readable .env")
  }
  if !strings.Contains(err.Error(), "chmod 600") {
    t.Fatalf("error = %v, want it to tell the user how to fix permissions", err)
  }
}

func TestEnvPathIsAlongsideConfigConf(t *testing.T) {
  dir := withConfigDir(t)

  envPath, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  want := filepath.Join(dir, "armin", ".env")
  if envPath != want {
    t.Fatalf("EnvPath() = %q, want %q", envPath, want)
  }
}

func TestPathHonorsXDGConfigHome(t *testing.T) {
  dir := withConfigDir(t)

  path, err := Path()
  if err != nil {
    t.Fatal(err)
  }
  want := filepath.Join(dir, "armin", "config.conf")
  if path != want {
    t.Fatalf("Path() = %q, want %q", path, want)
  }
}
