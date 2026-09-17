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

// Config holds every value a config file may override. Zero values
// mean "not set" — callers apply their own defaults on top.
type Config struct {
  Model           string
  CompletionModel string // used only by -lsp inline completion, not chat
  CtagsBinary     string // used only by -lsp definition/symbol/hover
  OllamaURL       string
  Root            string
  Timeout         time.Duration
  MaxOutput       int
  ExtraAllowed    []string // appended to sandbox.DefaultAllowlist()
}

// Path returns the config file location, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
  base := os.Getenv("XDG_CONFIG_HOME")
  if base == "" {
    home, err := os.UserHomeDir()
    if err != nil {
      return "", fmt.Errorf("\n\t... in Path:\n\t%w", err)
    }
    base = filepath.Join(home, ".config")
  }
  return filepath.Join(base, "armin", "config.conf"), nil
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
  switch key {
  case "model":
    cfg.Model = value
  case "completion_model":
    cfg.CompletionModel = value
  case "ctags_bin":
    cfg.CtagsBinary = value
  case "ollama_url":
    cfg.OllamaURL = value
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
