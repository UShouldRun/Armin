package llamacpp

import (
  "os"
  "path/filepath"
  "strings"
  "testing"
)

// fakeRoot lays out files (path → content) under a temp dir.
func fakeRoot(t *testing.T, files map[string]string) string {
  t.Helper()
  root := t.TempDir()
  for p, content := range files {
    full := filepath.Join(root, p)
    os.MkdirAll(filepath.Dir(full), 0o755)
    err := os.WriteFile(full, []byte(content), 0o644)
    if err != nil {
      t.Fatal(err)
    }
  }

  return root
}

func TestGPUWarning(t *testing.T) {
  gpu := map[string]string{
    "sys/bus/pci/devices/0000:01:00.0/vendor": "0x10de\n",
    "sys/bus/pci/devices/0000:01:00.0/class":  "0x030000\n",
    "sys/bus/pci/devices/0000:01:00.1/vendor": "0x10de\n",
    "sys/bus/pci/devices/0000:01:00.1/class":  "0x040300\n",
    "proc/sys/kernel/osrelease":               "6.8.0-142-generic\n",
  }

  with := func(extra map[string]string) map[string]string {
    out := make(map[string]string, len(gpu) + len(extra))
    for k, v := range gpu {
      out[k] = v
    }

    for k, v := range extra {
      out[k] = v
    }

    return out
  }

  cases := []struct {
    name  string
    files map[string]string
    want  string // a substring; "" means no warning
  }{
    {
      name: "no nvidia gpu",
      files: map[string]string{
        "sys/bus/pci/devices/0000:00:02.0/vendor": "0x8086\n",
        "sys/bus/pci/devices/0000:00:02.0/class":  "0x030000\n",
      },
    },
    {
      name: "audio function alone is not a gpu",
      files: map[string]string{
        "sys/bus/pci/devices/0000:01:00.1/vendor": "0x10de\n",
        "sys/bus/pci/devices/0000:01:00.1/class":  "0x040300\n",
      },
    },
    {
      name: "loaded and matching",
      files: with(map[string]string{
        "sys/module/nvidia/version":                       "595.84\n",
        "usr/lib/x86_64-linux-gnu/libnvidia-ml.so.595.84": "",
      }),
    },
    {
      name:  "no module for this kernel",
      files: with(map[string]string{
        "lib/modules/6.8.0-139-generic/kernel/nvidia-595-open/nvidia.ko": "",
      }),
      want: "sudo apt full-upgrade) and reboot, or pick 6.8.0-139-generic in GRUB",
    },
    {
      name:  "a newer kernel has one",
      files: with(map[string]string{
        "lib/modules/6.8.0-139-generic/kernel/nvidia-595-open/nvidia.ko":     "",
        "lib/modules/6.8.0-147-generic/kernel/nvidia-595-open/nvidia.ko.zst": "",
      }),
      want: "reboot into 6.8.0-147-generic",
    },
    {
      name:  "installed but not loaded",
      files: with(map[string]string{
        "lib/modules/6.8.0-142-generic/updates/dkms/nvidia.ko.zst": "",
      }),
      want: "installed for kernel 6.8.0-142-generic but not loaded",
    },
    {
      name:  "upgraded without a reboot",
      files: with(map[string]string{
        "sys/module/nvidia/version":                          "595.84\n",
        "usr/lib/x86_64-linux-gnu/libnvidia-ml.so.595.99.02": "",
      }),
      want: "loaded is 595.84 but its libraries are 595.99.02",
    },
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      got := gpuWarning(fakeRoot(t, c.files))
      if c.want == "" && got != "" {
        t.Fatalf("want no warning, got %q", got)
      }

      if !strings.Contains(got, c.want) {
        t.Fatalf("want %q in %q", c.want, got)
      }
    })
  }
}

func TestNewerKernel(t *testing.T) {
  cases := []struct {
    a, b string
    want bool
  }{
    { "6.8.0-147-generic", "6.8.0-142-generic", true },
    { "6.8.0-147-generic", "6.8.0-99-generic", true },
    { "6.8.0-99-generic", "6.8.0-147-generic", false },
    { "6.10.0-1-generic", "6.8.0-147-generic", true },
    { "6.8.0-142-generic", "6.8.0-142-generic", false },
  }

  for _, c := range cases {
    got := newerKernel(c.a, c.b)
    if got != c.want {
      t.Errorf("newerKernel(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
    }
  }
}
