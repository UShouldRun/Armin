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

// ModelSpecConfig configures one named model /model (or armin.model)
// can switch to — see internal/llamacpp.ModelSpec, which this is
// converted to. It's either external (URL set: armin only talks to
// the process there, never starts or stops it) or armin-managed
// (Binary/ModelPath/Port set: armin spawns a llama-server itself and
// reaps it when idle). Setting both isn't rejected here — this
// package does no cross-field validation, matching the rest of its
// minimalism — URL simply wins, same as
// internal/llamacpp.ModelSpec.managed() decides it.
type ModelSpecConfig struct {
  URL string

  Binary    string
  ModelPath string
  Port      int
  Args      []string

  IdleTimeout time.Duration
}

// Config holds every value a config file may override. Zero values
// mean "not set" — callers apply their own defaults on top.
type Config struct {
  Model           string // a name, resolved against Models (or LlamaURL, for the zero-config path)
  CompletionModel string // same, but for -lsp inline completion, not chat
  CtagsBinary     string // used only by -lsp definition/symbol/hover

  // LlamaURL/CompletionLlamaURL back the zero-config path: point
  // armin at an already-running llama-server without writing a
  // Models entry at all. Ignored for a name that does have a
  // matching Models entry.
  LlamaURL           string
  CompletionLlamaURL string

  // Models holds every "models.<name>.<field>" entry, keyed by
  // name — the fuller alternative to LlamaURL/CompletionLlamaURL,
  // needed for anything armin should spawn/manage itself, or for
  // naming more than the two built-in chat/completion slots.
  Models map[string]ModelSpecConfig

  Root         string
  Timeout      time.Duration
  MaxOutput    int
  ExtraAllowed []string // appended to sandbox.DefaultAllowlist()
}

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

    if err := apply(&cfg, key, value); err != nil {
      return Config{}, fmt.Errorf(
        "\n\t... in Load:\n\t %s:%d: %w", path, lineNo, err,
      )
    }
  }
  if err := scanner.Err(); err != nil {
    return Config{}, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  return cfg, nil
}

func apply(cfg *Config, key, value string) error {
  if name, field, ok := cutModelKey(key); ok {
    return applyModel(cfg, name, field, value)
  }

  switch key {
  case "model":
    cfg.Model = value
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
  case "allow":
    for _, bin := range strings.Split(value, ",") {
      bin = strings.TrimSpace(bin)
      if bin != "" {
        cfg.ExtraAllowed = append(cfg.ExtraAllowed, bin)
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
