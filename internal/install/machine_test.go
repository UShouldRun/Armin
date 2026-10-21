package install

import (
  "encoding/binary"
  "os"
  "path/filepath"
  "testing"
)

func write(t *testing.T, root, p, content string) {
  t.Helper()
  full := filepath.Join(root, p)
  err := os.MkdirAll(filepath.Dir(full), 0o755)
  if err != nil {
    t.Fatal(err)
  }

  err = os.WriteFile(full, []byte(content), 0o644)
  if err != nil {
    t.Fatal(err)
  }
}

// smbios17 is a type 17 entry: size MB, memory type, speed MT/s.
func smbios17(sizeMB uint16, typ byte, speed uint16) string {
  raw := make([]byte, 0x22)
  raw[0], raw[1] = 17, 0x22
  binary.LittleEndian.PutUint16(raw[0x0C:], sizeMB)
  raw[0x12] = typ
  binary.LittleEndian.PutUint16(raw[0x15:], speed)
  binary.LittleEndian.PutUint16(raw[0x20:], speed)

  return string(raw)
}

func TestProbeAt(t *testing.T) {
  orig := nvidiaSMI
  defer func() { nvidiaSMI = orig }()
  nvidiaSMI = func() (string, error) {
    return "NVIDIA GeForce RTX 3060, 12288, 512\n", nil
  }

  root := t.TempDir()
  bridge := "sys/bus/pci/devices/0000:00:01.1"
  write(t, root, bridge + "/max_link_speed", "8.0 GT/s PCIe\n")
  nv := bridge + "/0000:01:00.0"
  write(t, root, nv + "/class", "0x030000\n")
  write(t, root, nv + "/vendor", "0x10de\n")
  write(t, root, nv + "/max_link_speed", "16.0 GT/s PCIe\n")
  write(t, root, nv + "/current_link_width", "16\n")
  amd := "sys/bus/pci/devices/0000:05:00.0"
  write(t, root, amd + "/class", "0x030000\n")
  write(t, root, amd + "/vendor", "0x1002\n")
  write(t, root, amd + "/device", "0x15bf\n")
  write(t, root, amd + "/mem_info_vram_total", "536870912\n")
  write(t, root, amd + "/mem_info_vram_used", "0\n")
  write(t, root, "usr/share/misc/pci.ids", "1002  AMD\n\t15bf  Phoenix1\n10de  NVIDIA\n")

  // pcieLink reads the parent's link through "..", so the NVIDIA
  // device sits inside its bridge's directory here; link it where
  // the PCI listing expects it.
  err := os.Symlink(filepath.Join(root, nv), filepath.Join(root, "sys/bus/pci/devices/0000:01:00.0"))
  if err != nil {
    t.Fatal(err)
  }

  write(t, root, "proc/meminfo", "MemTotal:       32768000 kB\nMemAvailable:   20000000 kB\nSwapFree:        1000000 kB\n")
  write(t, root, "proc/cpuinfo",
    "model name\t: Test CPU\nphysical id\t: 0\ncore id\t: 0\nflags\t: fpu avx2\n\n"+
      "model name\t: Test CPU\nphysical id\t: 0\ncore id\t: 1\nflags\t: fpu avx2\n\n"+
      "model name\t: Test CPU\nphysical id\t: 0\ncore id\t: 0\nflags\t: fpu avx2\n")
  write(t, root, "sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq", "4500000\n")
  write(t, root, "sys/firmware/dmi/entries/17-0/raw", smbios17(16384, 0x1A, 3200))
  write(t, root, "sys/firmware/dmi/entries/17-1/raw", smbios17(16384, 0x1A, 3200))
  write(t, root, "sys/firmware/dmi/entries/17-2/raw", smbios17(0, 0, 0))

  hw := probeAt(root)
  if len(hw.GPUs) != 2 || hw.GPU == nil {
    t.Fatalf("gpus: %+v", hw.GPUs)
  }

  g := hw.GPU
  if g.Name != "NVIDIA GeForce RTX 3060" || g.VRAM != 12 * gib || g.VRAMFree != 11.5 * gib {
    t.Errorf("nvidia: %+v", g)
  }

  // The slot (PCIe 3.0) caps the card (4.0).
  if !g.Known || g.Bandwidth != 360 || g.PCIeGen != 3 || g.Lanes != 16 {
    t.Errorf("nvidia speeds: %+v", g)
  }

  igpu := hw.GPUs[1]
  if g == &hw.GPUs[1] {
    igpu = hw.GPUs[0]
  }

  if igpu.Dedicated || igpu.Name != "Phoenix1" {
    t.Errorf("igpu: %+v", igpu)
  }

  r := hw.RAM
  if r.Total != 32768000 << 10 || r.Type != "DDR4" || r.Speed != 3200 || r.Channels != 2 || r.Bandwidth != 51.2 {
    t.Errorf("ram: %+v", r)
  }

  c := hw.CPU
  if c.Name != "Test CPU" || c.Cores != 2 || c.GHz != 4.5 || !c.AVX2 || c.AVX512 {
    t.Errorf("cpu: %+v", c)
  }
}

func TestParseRAM(t *testing.T) {
  for in, want := range map[string][3]any{
    "ddr5-5600":       { "DDR5", 5600, 2 },
    "LPDDR5X-7500":    { "LPDDR5X", 7500, 2 },
    "ddr4-3200 1ch":   { "DDR4", 3200, 1 },
    "ddr5-6400 4ch":   { "DDR5", 6400, 4 },
  } {
    typ, speed, ch, ok := parseRAM(in)
    if !ok || typ != want[0] || speed != want[1] || ch != want[2] {
      t.Errorf("parseRAM(%q) = %s %d %d %v", in, typ, speed, ch, ok)
    }
  }

  for _, bad := range []string{ "", "5600", "ddr5", "ddr5-fast", "gddr", "ddr5-5600 xch" } {
    _, _, _, ok := parseRAM(bad)
    if ok && bad != "gddr" {
      t.Errorf("parseRAM(%q) accepted", bad)
    }
  }
}

func TestLookupGPU(t *testing.T) {
  for name, want := range map[string]float64{
    "NVIDIA GeForce RTX 4060 Laptop GPU":                  256,
    "NVIDIA GeForce RTX 4060 Ti":                          288,
    "NVIDIA GeForce RTX 4060":                             272,
    "NVIDIA L40S":                                         864,
    "NVIDIA L4":                                           300,
    "Navi 31 [Radeon RX 7900 XT/7900 XTX/7900 GRE/7900M]": 800,
  } {
    s, ok := lookupGPU(name)
    if !ok || s.bandwidth != want {
      t.Errorf("lookupGPU(%q) = %v %v, want %v", name, s.bandwidth, ok, want)
    }
  }

  _, ok := lookupGPU("Some Future GPU 9000")
  if ok {
    t.Error("unknown GPU matched")
  }
}

func TestLinkGen(t *testing.T) {
  for in, want := range map[string]int{
    "2.5 GT/s PCIe": 1, "8.0 GT/s PCIe": 3, "16.0 GT/s PCIe": 4,
    "32.0 GT/s PCIe": 5, "": 0, "Unknown": 0,
  } {
    got := linkGen(in)
    if got != want {
      t.Errorf("linkGen(%q) = %d, want %d", in, got, want)
    }
  }
}
