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

func TestRegionalFor(t *testing.T) {
  const base = "https://openrouter.ai/api/v1"
  cases := []struct{ setting, want string }{
    { "auto", Regional(base) }, // by time zone, like Regional
    { "off", base },            // most accounts can't use the regional hosts
    { "eu", "https://eu.openrouter.ai/api/v1" },
    { "us", "https://us.openrouter.ai/api/v1" },
    { "nonsense", base },
  }

  for _, c := range cases {
    got := RegionalFor(base, c.setting)
    if got != c.want {
      t.Errorf("RegionalFor(%q, %q) = %q, want %q", base, c.setting, got, c.want)
    }
  }

  // unset is auto: by time zone, whatever that resolves to here
  if RegionalFor(base, "") != Regional(base) {
    t.Errorf("RegionalFor(base, \"\") is not Regional(base)")
  }

  // a forced region leaves a non-OpenRouter URL alone
  if got := RegionalFor("https://api.anthropic.com/v1", "eu"); got != "https://api.anthropic.com/v1" {
    t.Errorf("RegionalFor on another provider = %q", got)
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
