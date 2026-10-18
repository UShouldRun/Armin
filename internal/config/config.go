// Package config loads user defaults from
// ~/.config/armin/config.conf (or $XDG_CONFIG_HOME/armin/config.conf)
// — model name, allowlist additions, timeouts — so they don't have
// to be typed as flags every run (plan Phase 4). The format is
// deliberately hand-rolled and tiny: "key = value" lines, "#"
// comments, nothing fancier.
package config

import (
  "bufio"
  "fmt"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "time"
)

// Dir returns the armin config directory, honoring XDG_CONFIG_HOME —
// the same directory config.conf lives in, exported so other
// packages (e.g. internal/searxng, for its generated settings.yml)
// have one place to put their own on-disk state instead of each
// re-deriving XDG_CONFIG_HOME themselves.
func Dir() (string, error) {
  base := os.Getenv("XDG_CONFIG_HOME")
  if base == "" {
    home, err := os.UserHomeDir()
    if err != nil {
      return "", fmt.Errorf("\n\t... in Dir:\n\t%w", err)
    }

    base = filepath.Join(home, ".config")
  }

  return filepath.Join(base, "armin"), nil
}

// Path returns the config file location, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
  dir, err := Dir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in Path:\n\t%w", err)
  }

  return filepath.Join(dir, "config.conf"), nil
}

// EnvPath returns the .env file location, honoring XDG_CONFIG_HOME —
// the same directory config.conf lives in, so a models.<name>.url
// entry pointing at an authenticated remote endpoint (Claude's
// OpenAI-compatible API, say) has somewhere to keep its key out of
// config.conf's own plain text.
func EnvPath() (string, error) {
  dir, err := Dir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in EnvPath:\n\t%w", err)
  }

  return filepath.Join(dir, ".env"), nil
}

// LoadEnv reads .env (see EnvPath) if it exists and calls os.Setenv
// for each "KEY=value" line it finds — but only for a key that isn't
// already set in the real environment, so an explicitly exported
// shell variable always wins over the file, the usual dotenv
// precedence. A missing file is not an error. Deliberately as
// minimal as config.conf's own parser: no quoting, no "export "
// prefix, no variable expansion — just "key = value" and "#"
// comments. cmd/armin/main.go calls this once, at startup, before
// resolving any ModelSpecConfig.APIKeyEnv.
//
// Refuses to read a file that's group- or other-readable: this file
// is meant for API keys, and a wrong umask leaving it world-readable
// is exactly the kind of mistake that should fail loud rather than
// quietly work.
func LoadEnv() error {
  path, err := EnvPath()
  if err != nil {
    return fmt.Errorf("\n\t... in LoadEnv:\n\t%w", err)
  }

  info, err := os.Stat(path)
  if os.IsNotExist(err) {
    return nil
  }

  if err != nil {
    return fmt.Errorf("\n\t... in LoadEnv:\n\t%w", err)
  }

  perm := info.Mode().Perm()
  if perm & 0o077 != 0 {
    return fmt.Errorf(
      "\n\t... in LoadEnv:\n\t %s is readable by group/other (mode %#o) "+
        "— it may hold API keys; fix this before armin will read it: "+
        "chmod 600 %s",
      path, perm, path,
    )
  }

  f, err := os.Open(path)
  if err != nil {
    return fmt.Errorf("\n\t... in LoadEnv:\n\t%w", err)
  }

  defer f.Close()

  scanner := bufio.NewScanner(f)
  lineNo := 0

  for scanner.Scan() {
    lineNo++
    line := strings.TrimSpace(scanner.Text())
    if line == "" || strings.HasPrefix(line, "#") {
      continue
    }

    key, value, ok := strings.Cut(line, "=")
    if !ok {
      return fmt.Errorf(
        "\n\t... in LoadEnv:\n\t %s:%d: expected \"KEY=value\", got %q",
        path, lineNo, line,
      )
    }

    key = strings.TrimSpace(key)
    value = strings.TrimSpace(value)

    _, alreadySet := os.LookupEnv(key)
    if alreadySet {
      continue
    }

    err := os.Setenv(key, value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in LoadEnv:\n\t setting %s: %w", key, err,
      )
    }
  }

  err = scanner.Err()
  if err != nil {
    return fmt.Errorf("\n\t... in LoadEnv:\n\t%w", err)
  }

  return nil
}

// Load reads the config file if it exists. A missing file is not
// an error — it just means every field comes back zero-valued.
func Load() (Config, error) {
  path, err := Path()
  if err != nil {
    return Config{}, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  f, err := os.Open(path)
  if os.IsNotExist(err) {
    return Config{}, nil
  }

  if err != nil {
    return Config{}, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  defer f.Close()

  var cfg Config
  scanner := bufio.NewScanner(f)
  lineNo := 0

  for scanner.Scan() {
    lineNo++
    line := strings.TrimSpace(scanner.Text())
    if line == "" || strings.HasPrefix(line, "#") {
      continue
    }

    key, value, ok := strings.Cut(line, "=")
    if !ok {
      return Config{}, fmt.Errorf(
        "\n\t... in Load:\n\t %s:%d: expected \"key = value\", got %q",
        path, lineNo, line,
      )
    }

    key = strings.TrimSpace(key)
    value = strings.TrimSpace(value)

    err := apply(&cfg, key, value)
    if err != nil {
      return Config{}, fmt.Errorf(
        "\n\t... in Load:\n\t %s:%d: %w", path, lineNo, err,
      )
    }
  }

  err = scanner.Err()
  if err != nil {
    return Config{}, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  return cfg, nil
}

func apply(cfg *Config, key, value string) error {
  name, field, ok := cutModelKey(key)
  if ok {
    return applyModel(cfg, name, field, value)
  }

  switch key {
  case "model":
    cfg.Model = value
  case "terminal":
    cfg.Terminal = value
  case "completion_model":
    cfg.CompletionModel = value
  case "ctags_bin":
    cfg.CtagsBinary = value
  case "llama_url":
    cfg.LlamaURL = value
  case "completion_llama_url":
    cfg.CompletionLlamaURL = value
  case "root":
    cfg.Root = value
  case "timeout":
    d, err := time.ParseDuration(value)
    if err != nil {
      return fmt.Errorf("\n\t... in apply:\n\t timeout: %w", err)
    }

    cfg.Timeout = d
  case "max_output":
    n, err := strconv.Atoi(value)
    if err != nil {
      return fmt.Errorf("\n\t... in apply:\n\t max_output: %w", err)
    }

    cfg.MaxOutput = n
  case "max_tool_rounds", "context_size", "keep_turns":
    n, err := strconv.Atoi(value)
    if err != nil {
      return fmt.Errorf("\n\t... in apply:\n\t %s: %w", key, err)
    }

    switch key {
    case "max_tool_rounds":
      cfg.MaxToolRounds = n
    case "context_size":
      cfg.ContextSize = n
    default:
      cfg.KeepTurns = n
    }
  case "read_timeout", "build_timeout":
    d, err := time.ParseDuration(value)
    if err != nil {
      return fmt.Errorf("\n\t... in apply:\n\t %s: %w", key, err)
    }

    if key == "read_timeout" {
      cfg.ReadTimeout = d
    } else {
      cfg.BuildTimeout = d
    }
  case "auto_approve":
    b, err := strconv.ParseBool(value)
    if err != nil {
      return fmt.Errorf("\n\t... in apply:\n\t auto_approve: %w", err)
    }

    cfg.AutoApprove = b
  case "aids":
    cfg.Aids = value
  case "effort":
    cfg.Effort = value
  case "library":
    cfg.Library = value
  case "test_cmd":
    cfg.TestCmd = strings.Fields(value)
  case "build_cmd":
    cfg.BuildCmd = strings.Fields(value)
  case "lint_cmd":
    cfg.LintCmd = strings.Fields(value)
  case "allow":
    for _, bin := range strings.Split(value, ",") {
      bin = strings.TrimSpace(bin)
      if bin != "" {
        cfg.ExtraAllowed = append(cfg.ExtraAllowed, bin)
      }
    }
  case "allow_exec":
    for _, bin := range strings.Split(value, ",") {
      bin = strings.TrimSpace(bin)
      if bin != "" {
        cfg.ExtraExec = append(cfg.ExtraExec, bin)
      }
    }
  default:
    return fmt.Errorf("\n\t... in apply:\n\t unknown config key %q", key)
  }

  return nil
}

// cutModelKey splits a "models.<name>.<field>" key into name and
// field. A model name itself may not contain a ".", same restriction
// bufio-line keys already implicitly have.
func cutModelKey(key string) (name, field string, ok bool) {
  rest, ok := strings.CutPrefix(key, "models.")
  if !ok {
    return "", "", false
  }

  name, field, ok = strings.Cut(rest, ".")

  return name, field, ok
}

func applyModel(cfg *Config, name, field, value string) error {
  if cfg.Models == nil {
    cfg.Models = make(map[string]ModelSpecConfig)
  }

  spec := cfg.Models[name]

  switch field {
  case "url":
    spec.URL = value
  case "binary":
    spec.Binary = value
  case "model_path":
    spec.ModelPath = value
  case "api_key_env":
    spec.APIKeyEnv = value
  case "id":
    spec.ID = value
  case "reasoning":
    spec.Reasoning = value
  case "aws_instance":
    spec.AWSInstance = value
  case "aws_region":
    spec.AWSRegion = value
  case "ssh_user":
    spec.SSHUser = value
  case "ssh_key":
    spec.SSHKey = value
  case "remote_port":
    n, err := strconv.Atoi(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.remote_port: %w", name, err,
      )
    }

    spec.RemotePort = n
  case "vision":
    b, err := strconv.ParseBool(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.vision: %w", name, err,
      )
    }

    spec.Vision = b
  case "stream_binary":
    spec.StreamBinary = value
  case "stream_context", "stream_arena_mib":
    n, err := strconv.Atoi(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.%s: %w", name, field, err,
      )
    }

    if field == "stream_context" {
      spec.StreamContext = n
    } else {
      spec.StreamArenaMiB = n
    }
  case "stream_args":
    spec.StreamArgs = append(spec.StreamArgs, strings.Fields(value)...)
  case "scaleway_server":
    spec.ScalewayServer = value
  case "scaleway_zone":
    spec.ScalewayZone = value
  case "lean":
    b, err := strconv.ParseBool(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.lean: %w", name, err,
      )
    }

    spec.NoLean = !b
  case "stackit_server":
    spec.StackitServer = value
  case "stackit_project":
    spec.StackitProject = value
  case "stackit_region":
    spec.StackitRegion = value
  case "stop_on_disconnect", "aws_stop_on_disconnect":
    b, err := strconv.ParseBool(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.%s: %w", name, field, err,
      )
    }

    spec.KeepOnDisconnect = !b
  case "port":
    n, err := strconv.Atoi(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.port: %w", name, err,
      )
    }

    spec.Port = n
  case "idle_timeout":
    d, err := time.ParseDuration(value)
    if err != nil {
      return fmt.Errorf(
        "\n\t... in applyModel:\n\t models.%s.idle_timeout: %w",
        name, err,
      )
    }

    spec.IdleTimeout = d
  case "args":
    // Whitespace-split, not comma-split like "allow" above: this is
    // an ordered CLI flag sequence ("--ctx-size 8192
    // --n-gpu-layers 999"), not a set of independent names — no
    // quoting support, same minimalism as the rest of this parser.
    spec.Args = append(spec.Args, strings.Fields(value)...)
  default:
    return fmt.Errorf(
      "\n\t... in applyModel:\n\t unknown model config key \"models.%s.%s\"",
      name, field,
    )
  }

  cfg.Models[name] = spec

  return nil
}
