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
  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    t.Fatal(err)
  }
  err = os.WriteFile(path, []byte(contents), 0o644)
  if err != nil {
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
    ExtraAllowed:       []string{ "curl", "docker" },
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
  if fim.IdleTimeout != 5 * time.Minute {
    t.Fatalf("fim.IdleTimeout = %v, want 5m", fim.IdleTimeout)
  }
  want := []string{ "--ctx-size", "8192", "--n-gpu-layers", "999" }
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

  _, err := Load()
  if err == nil {
    t.Fatal("expected an error for an unknown models.<name>.<field> key")
  }
}

func TestLoadRejectsBadModelPort(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "models.fim.port = not-a-number\n")

  _, err := Load()
  if err == nil {
    t.Fatal("expected an error for an unparseable models.<name>.port")
  }
}

func TestLoadRejectsUnknownKey(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "not_a_real_key = value\n")

  _, err := Load()
  if err == nil {
    t.Fatal("expected an error for an unknown config key")
  }
}

func TestLoadRejectsMalformedLine(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "this line has no equals sign\n")

  _, err := Load()
  if err == nil {
    t.Fatal("expected an error for a line with no \"key = value\" shape")
  }
}

func TestLoadRejectsBadTimeout(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "timeout = not-a-duration\n")

  _, err := Load()
  if err == nil {
    t.Fatal("expected an error for an unparseable timeout")
  }
}

func TestLoadRejectsBadMaxOutput(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, "max_output = not-a-number\n")

  _, err := Load()
  if err == nil {
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
  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    t.Fatal(err)
  }
  err = os.WriteFile(path, []byte("FOO_KEY=abc123\n# a comment\n\nBAR_KEY = spaced \n"), 0o600)
  if err != nil {
    t.Fatal(err)
  }

  os.Unsetenv("FOO_KEY")
  os.Unsetenv("BAR_KEY")
  t.Cleanup(func() { os.Unsetenv("FOO_KEY"); os.Unsetenv("BAR_KEY") })

  err = LoadEnv()
  if err != nil {
    t.Fatalf("LoadEnv: %v", err)
  }

  got := os.Getenv("FOO_KEY")
  if got != "abc123" {
    t.Fatalf("FOO_KEY = %q, want abc123", got)
  }
  got = os.Getenv("BAR_KEY")
  if got != "spaced" {
    t.Fatalf("BAR_KEY = %q, want \"spaced\" (trimmed)", got)
  }
}

func TestLoadEnvDoesNotOverrideRealEnvironment(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    t.Fatal(err)
  }
  err = os.WriteFile(path, []byte("PRESET_KEY=from-dotenv\n"), 0o600)
  if err != nil {
    t.Fatal(err)
  }

  t.Setenv("PRESET_KEY", "from-real-env")

  err = LoadEnv()
  if err != nil {
    t.Fatalf("LoadEnv: %v", err)
  }
  got := os.Getenv("PRESET_KEY")
  if got != "from-real-env" {
    t.Fatalf("PRESET_KEY = %q, want the real environment to win over .env", got)
  }
}

func TestLoadEnvMissingFileIsNotError(t *testing.T) {
  withConfigDir(t)
  err := LoadEnv()
  if err != nil {
    t.Fatalf("LoadEnv on a missing .env returned an error: %v", err)
  }
}

func TestLoadEnvRejectsMalformedLine(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    t.Fatal(err)
  }
  err = os.WriteFile(path, []byte("not a key value line\n"), 0o600)
  if err != nil {
    t.Fatal(err)
  }

  err = LoadEnv()
  if err == nil {
    t.Fatal("expected an error for a line with no \"KEY=value\" shape")
  }
}

func TestLoadEnvRejectsLoosePermissions(t *testing.T) {
  withConfigDir(t)
  path, err := EnvPath()
  if err != nil {
    t.Fatal(err)
  }
  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    t.Fatal(err)
  }
  err = os.WriteFile(path, []byte("FOO_KEY=abc123\n"), 0o644)
  if err != nil {
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

func TestLoadParsesAgentKeys(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
max_tool_rounds = 80
context_size    = 65536
keep_turns      = -1
read_timeout    = 10m
build_timeout   = 20m
auto_approve    = true
test_cmd        = go test -race ./...
`)

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  if cfg.MaxToolRounds != 80 || cfg.ContextSize != 65536 || cfg.KeepTurns != -1 {
    t.Errorf("ints = %d %d %d", cfg.MaxToolRounds, cfg.ContextSize, cfg.KeepTurns)
  }
  if cfg.ReadTimeout != 10 * time.Minute || cfg.BuildTimeout != 20 * time.Minute {
    t.Errorf("durations = %s %s", cfg.ReadTimeout, cfg.BuildTimeout)
  }
  if !cfg.AutoApprove {
    t.Error("AutoApprove = false")
  }
  if strings.Join(cfg.TestCmd, " ") != "go test -race ./..." {
    t.Errorf("TestCmd = %v", cfg.TestCmd)
  }
}

func TestLoadParsesAWSModel(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
models.qwen.aws_instance           = i-0abc
models.qwen.aws_region             = eu-west-1
models.qwen.ssh_user               = ubuntu
models.qwen.ssh_key                = ~/.ssh/gpu.pem
models.qwen.remote_port            = 8080
models.qwen.aws_stop_on_disconnect = false
models.qwen.model_path             = /models/q.gguf
`)

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  q := cfg.Models["qwen"]
  if q.AWSInstance != "i-0abc" || q.AWSRegion != "eu-west-1" || q.SSHUser != "ubuntu" ||
    q.SSHKey != "~/.ssh/gpu.pem" || q.RemotePort != 8080 || !q.KeepOnDisconnect {
    t.Fatalf("qwen = %+v", q)
  }
}

func TestLoadParsesPrivate(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
models.planner.url     = https://openrouter.ai/api/v1
models.planner.private = true
`)

  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }

  if !cfg.Models["planner"].Private {
    t.Fatalf("planner = %+v", cfg.Models["planner"])
  }

  writeConfig(t, "models.planner.private = maybe\n")
  _, err = Load()
  if err == nil {
    t.Fatal("private = maybe was accepted")
  }
}

func TestLoadParsesStackitModel(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
models.gpu.stackit_server     = 0b8a-server
models.gpu.stackit_project    = 9f1c-project
models.gpu.stop_on_disconnect = true
`)
  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  g := cfg.Models["gpu"]
  if g.StackitServer != "0b8a-server" || g.StackitProject != "9f1c-project" || g.KeepOnDisconnect {
    t.Fatalf("gpu = %+v", g)
  }
}

func TestLoadParsesScalewayModel(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
models.l40s.scaleway_server = 5f2e-server
models.l40s.scaleway_zone   = fr-par-2
`)
  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  l := cfg.Models["l40s"]
  if l.ScalewayServer != "5f2e-server" || l.ScalewayZone != "fr-par-2" {
    t.Fatalf("l40s = %+v", l)
  }
}

func TestLoadParsesStreamTier(t *testing.T) {
  withConfigDir(t)
  writeConfig(t, `
models.local.args             = --jinja -c 98304
models.local.stream_binary    = /opt/kvstream/llama-server
models.local.stream_context   = 196608
models.local.stream_arena_mib = 2048
models.local.stream_args      = --fit off -b 256 -ub 256
`)
  cfg, err := Load()
  if err != nil {
    t.Fatalf("Load: %v", err)
  }
  l := cfg.Models["local"]
  if l.StreamBinary != "/opt/kvstream/llama-server" || l.StreamContext != 196608 ||
    l.StreamArenaMiB != 2048 || strings.Join(l.StreamArgs, " ") != "--fit off -b 256 -ub 256" {
    t.Fatalf("local = %+v", l)
  }
}
