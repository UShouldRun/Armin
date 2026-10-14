package catalog

import (
  "os"
  "path/filepath"
  "testing"
)

func TestRegional(t *testing.T) {
  cases := []struct{ base, tz, want string }{
    { "https://openrouter.ai/api/v1", "Europe/Lisbon", "https://eu.openrouter.ai/api/v1" },
    { "https://openrouter.ai/api", "Atlantic/Azores", "https://eu.openrouter.ai/api" },
    { "https://openrouter.ai/api/v1", "America/Chicago", "https://us.openrouter.ai/api/v1" },
    { "https://openrouter.ai/api/v1", "US/Pacific", "https://us.openrouter.ai/api/v1" },
    { "https://openrouter.ai/api/v1", "America/Toronto", "https://openrouter.ai/api/v1" },
    { "https://openrouter.ai/api/v1", "America/Sao_Paulo", "https://openrouter.ai/api/v1" },
    { "https://openrouter.ai/api/v1", "Europe/London", "https://openrouter.ai/api/v1" },
    { "https://openrouter.ai/api/v1", "", "https://openrouter.ai/api/v1" },
    { "https://eu.openrouter.ai/api/v1", "Europe/Berlin", "https://eu.openrouter.ai/api/v1" },
    { "https://api.anthropic.com/v1", "Europe/Paris", "https://api.anthropic.com/v1" },
    { "http://localhost:8080", "Europe/Paris", "http://localhost:8080" },
  }

  for _, c := range cases {
    got := regional(c.base, c.tz)
    if got != c.want {
      t.Errorf("regional(%q, %q) = %q, want %q", c.base, c.tz, got, c.want)
    }
  }
}

func TestGlobal(t *testing.T) {
  cases := []struct{ base, want string }{
    { "https://eu.openrouter.ai/api/v1", "https://openrouter.ai/api/v1" },
    { "https://us.openrouter.ai/api", "https://openrouter.ai/api" },
    { "https://openrouter.ai/api/v1", "" },
    { "https://eu.example.com/v1", "" },
    { "https://api.anthropic.com/v1", "" },
    { "http://localhost:8080", "" },
  }

  for _, c := range cases {
    got := Global(c.base)
    if got != c.want {
      t.Errorf("Global(%q) = %q, want %q", c.base, got, c.want)
    }
  }
}

func TestZone(t *testing.T) {
  t.Setenv("TZ", "")
  root := t.TempDir()
  etc := filepath.Join(root, "etc")
  os.MkdirAll(etc, 0o755)

  got := zone(root)
  if got != "" {
    t.Fatalf("empty root: zone = %q", got)
  }

  os.Symlink("/usr/share/zoneinfo/Europe/Lisbon", filepath.Join(etc, "localtime"))
  got = zone(root)
  if got != "Europe/Lisbon" {
    t.Fatalf("localtime link: zone = %q", got)
  }

  os.WriteFile(filepath.Join(etc, "timezone"), []byte("Europe/Madrid\n"), 0o644)
  got = zone(root)
  if got != "Europe/Madrid" {
    t.Fatalf("etc/timezone: zone = %q", got)
  }

  t.Setenv("TZ", ":Europe/Rome")
  got = zone(root)
  if got != "Europe/Rome" {
    t.Fatalf("TZ: zone = %q", got)
  }
}
