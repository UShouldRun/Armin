// What the computer has, read from Linux: GPUs from sysfs (and
// nvidia-smi for NVIDIA memory), system RAM from /proc/meminfo and
// its type and speed from SMBIOS (root-only, so often asked for
// instead), the CPU from /proc/cpuinfo, and free disk space.

package install

import (
  "encoding/binary"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strconv"
  "strings"
  "syscall"

  "armin/internal/llamacpp"
)

const (
  VendorOther Vendor = iota
  VendorNVIDIA
  VendorAMD
  VendorIntel
)

const (
  gib            = 1 << 30
  integratedVRAM = 4 * gib // an AMD "VRAM" this small is an APU's carve-out
)

// Assumed values for a GPU the table doesn't know.
const (
  assumedBandwidth = 250.0
  assumedTFLOPS    = 20.0
)

// smbiosTypes are SMBIOS type 17's memory types armin names.
var smbiosTypes = map[byte]string{
  0x12: "DDR", 0x13: "DDR2", 0x18: "DDR3", 0x1A: "DDR4", 0x1B: "LPDDR",
  0x1C: "LPDDR2", 0x1D: "LPDDR3", 0x1E: "LPDDR4", 0x22: "DDR5",
  0x23: "LPDDR5",
}

// nvidiaSMI runs nvidia-smi's query; a variable so tests can stand in.
var nvidiaSMI = func() (string, error) {
  out, err := exec.Command(
    "nvidia-smi", "--query-gpu=name,memory.total,memory.used",
    "--format=csv,noheader,nounits",
  ).Output()

  return string(out), err
}

// probe reads this machine; dir is where the model will be saved.
func probe(dir string) Machine {
  m := probeAt("/")
  m.DiskFree = diskFree(dir)

  return m
}

// probeAt is probe against a filesystem rooted at root, for tests.
func probeAt(root string) Machine {
  at := func(p string) string { return filepath.Join(root, p) }

  var m Machine
  m.GPUs = gpus(at)
  for i := range m.GPUs {
    g := &m.GPUs[i]
    if g.Dedicated && g.Problem == "" && (m.GPU == nil || g.VRAM > m.GPU.VRAM) {
      m.GPU = g
    }
  }

  m.RAM = meminfo(readFile(at("proc/meminfo")))
  smbiosRAM(&m.RAM, at("sys/firmware/dmi/entries"))
  m.CPU = cpuinfo(
    readFile(at("proc/cpuinfo")),
    readFile(at("sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq")),
  )

  return m
}

// gpus lists display controllers (PCI class 0x03), dedicated or not,
// with their memory, bus and, from the table, their speed.
func gpus(at func(string) string) []GPU {
  devices := at("sys/bus/pci/devices")
  entries, err := os.ReadDir(devices)
  if err != nil {
    return nil
  }

  var out []GPU
  smiDone := false
  var smi []string
  for _, e := range entries {
    dir := filepath.Join(devices, e.Name())
    class := strings.TrimSpace(readFile(filepath.Join(dir, "class")))
    if !strings.HasPrefix(class, "0x03") {
      continue
    }

    g := GPU{}
    vendor := strings.TrimSpace(readFile(filepath.Join(dir, "vendor")))
    switch vendor {
    case "0x10de":
      g.Vendor, g.Dedicated = VendorNVIDIA, true
      if !smiDone {
        smi, smiDone = nvidiaRows(), true
      }

      if len(smi) > 0 {
        g.Name, g.VRAM, g.VRAMFree = nvidiaRow(smi[0])
        smi = smi[1:]
      } else {
        g.Problem = llamacpp.GPUWarning()
        if g.Problem == "" {
          g.Problem = "nvidia-smi didn't answer, so the driver " +
            "probably isn't working: models would run on the CPU"
        }
      }
    case "0x1002":
      g.Vendor = VendorAMD
      total := atoi64(readFile(filepath.Join(dir, "mem_info_vram_total")))
      used := atoi64(readFile(filepath.Join(dir, "mem_info_vram_used")))
      g.VRAM, g.VRAMFree = total, total - used
      g.Dedicated = total >= integratedVRAM
    case "0x8086":
      g.Vendor = VendorIntel
    default:
      continue
    }

    if g.Name == "" {
      g.Name = pciName(at, vendor, readFile(filepath.Join(dir, "device")))
    }

    g.PCIeGen, g.Lanes = pcieLink(dir)
    spec, ok := lookupGPU(g.Name)
    g.Known = ok
    g.Bandwidth, g.TFLOPS = spec.bandwidth, spec.tflops
    if !ok {
      g.Bandwidth, g.TFLOPS = assumedBandwidth, assumedTFLOPS
    }

    out = append(out, g)
  }

  return out
}

func nvidiaRows() []string {
  out, err := nvidiaSMI()
  if err != nil {
    return nil
  }

  var rows []string
  for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
    if strings.Count(line, ",") == 2 {
      rows = append(rows, line)
    }
  }

  return rows
}

// nvidiaRow parses "name, total MiB, used MiB".
func nvidiaRow(row string) (string, int64, int64) {
  f := strings.Split(row, ",")
  total := atoi64(f[1]) << 20
  used := atoi64(f[2]) << 20

  return strings.TrimSpace(f[0]), total, total - used
}

// pciName looks the device up in pci.ids, when the system has it.
func pciName(at func(string) string, vendor, device string) string {
  v := strings.TrimPrefix(strings.TrimSpace(vendor), "0x")
  d := strings.TrimPrefix(strings.TrimSpace(device), "0x")
  for _, p := range []string{
    "usr/share/hwdata/pci.ids", "usr/share/misc/pci.ids",
  } {
    ids := "\n" + readFile(at(p))
    if len(ids) == 1 {
      continue
    }

    i := strings.Index(ids, "\n" + v + "  ")
    if i < 0 {
      continue
    }

    // The vendor's devices follow it, indented, until the next vendor.
    for _, line := range strings.Split(ids[i + 1:], "\n")[1:] {
      if line == "" || line[0] != '\t' && line[0] != '#' {
        break
      }

      if strings.HasPrefix(line, "\t" + d + "  ") {
        return strings.TrimSpace(line[len(d) + 3:])
      }
    }
  }

  return "unknown GPU " + v + ":" + d
}

// pcieLink is the GPU's link: the generation the GPU and its slot
// both reach, and the lanes it trained to.
func pcieLink(dir string) (int, int) {
  // The PCI listing holds symlinks; the slot's bridge is the parent
  // of where one points, not of the link.
  real, err := filepath.EvalSymlinks(dir)
  if err != nil {
    real = dir
  }

  gen := linkGen(readFile(filepath.Join(dir, "max_link_speed")))
  slot := linkGen(readFile(filepath.Join(filepath.Dir(real), "max_link_speed")))
  if slot > 0 && slot < gen {
    gen = slot
  }

  lanes := int(atoi64(readFile(filepath.Join(dir, "current_link_width"))))

  return gen, lanes
}

// linkGen maps "16.0 GT/s PCIe" to generation 4.
func linkGen(s string) int {
  f := strings.Fields(s)
  if len(f) == 0 {
    return 0
  }

  gts, _ := strconv.ParseFloat(f[0], 64)
  switch {
  case gts >= 64:
    return 6
  case gts >= 32:
    return 5
  case gts >= 16:
    return 4
  case gts >= 8:
    return 3
  case gts >= 5:
    return 2
  case gts > 0:
    return 1
  }

  return 0
}

// PCIeBandwidth is GB/s one way over gen × lanes.
func PCIeBandwidth(gen, lanes int) float64 {
  perLane := []float64{ 0, 0.25, 0.5, 0.985, 1.97, 3.94, 7.56 }
  if gen <= 0 || gen >= len(perLane) || lanes <= 0 {
    return 0
  }

  return perLane[gen] * float64(lanes)
}

// meminfo reads total, available and free swap.
func meminfo(s string) RAM {
  var r RAM
  for _, line := range strings.Split(s, "\n") {
    key, val, ok := strings.Cut(line, ":")
    if !ok {
      continue
    }

    kb := atoi64(strings.TrimSuffix(strings.TrimSpace(val), " kB")) << 10
    switch key {
    case "MemTotal":
      r.Total = kb
    case "MemAvailable":
      r.Available = kb
    case "SwapFree":
      r.SwapFree = kb
    }
  }

  return r
}

// smbiosRAM reads the memory devices' type, speed and count from
// SMBIOS type 17 entries; readable only by root, so usually it
// leaves r untouched for the caller to ask or assume.
func smbiosRAM(r *RAM, entries string) {
  dirs, _ := filepath.Glob(filepath.Join(entries, "17-*"))
  populated := 0
  for _, d := range dirs {
    raw, err := os.ReadFile(filepath.Join(d, "raw"))
    if err != nil || len(raw) < 0x17 || raw[0] != 17 {
      continue
    }

    size := binary.LittleEndian.Uint16(raw[0x0C:])
    if size == 0 || size == 0xFFFF {
      continue
    }

    populated++
    speed := int(binary.LittleEndian.Uint16(raw[0x15:]))
    if len(raw) >= 0x22 {
      configured := int(binary.LittleEndian.Uint16(raw[0x20:]))
      if configured > 0 && configured != 0xFFFF {
        speed = configured
      }
    }

    if r.Type == "" {
      r.Type = smbiosTypes[raw[0x12]]
    }

    if speed > 0 && speed != 0xFFFF && (r.Speed == 0 || speed < r.Speed) {
      r.Speed = speed
    }
  }

  if populated == 0 || r.Speed == 0 {
    return
  }

  // Consumer boards are dual-channel: four DIMMs share two channels.
  r.Channels = populated
  if r.Channels > 2 {
    r.Channels = 2
  }

  r.Source = "SMBIOS"
  r.Bandwidth = ramBandwidth(r.Speed, r.Channels)
}

// ramBandwidth is GB/s for 64-bit channels at speed MT/s.
func ramBandwidth(speed, channels int) float64 {
  return float64(speed) * 8 * float64(channels) / 1000
}

// parseRAM reads "ddr5-5600", "lpddr5x-7500 2ch" or "ddr4-3200 1ch".
func parseRAM(s string) (typ string, speed, channels int, ok bool) {
  f := strings.Fields(strings.ToUpper(strings.TrimSpace(s)))
  if len(f) == 0 {
    return "", 0, 0, false
  }

  typ, sp, found := strings.Cut(f[0], "-")
  if !found || !strings.Contains(typ, "DDR") {
    return "", 0, 0, false
  }

  speed, err := strconv.Atoi(sp)
  if err != nil || speed < 200 || speed > 20000 {
    return "", 0, 0, false
  }

  channels = 2
  if len(f) > 1 {
    n, err := strconv.Atoi(strings.TrimSuffix(f[1], "CH"))
    if err != nil || n < 1 || n > 16 {
      return "", 0, 0, false
    }

    channels = n
  }

  return typ, speed, channels, true
}

// guessRAM is the assumption when nothing better is known: DDR5 on
// CPUs new enough to have AVX-512 or AVX-VNNI, DDR4 before.
func guessRAM(c CPU) string {
  if c.AVX512 {
    return "ddr5-5600"
  }

  return "ddr4-3200"
}

// cpuinfo reads the model, physical cores, SIMD width and peak clock.
func cpuinfo(s, maxKHz string) CPU {
  var c CPU
  cores := map[string]bool{}
  phys := "0"
  for _, line := range strings.Split(s, "\n") {
    key, val, ok := strings.Cut(line, ":")
    if !ok {
      continue
    }

    key, val = strings.TrimSpace(key), strings.TrimSpace(val)
    switch key {
    case "model name":
      if c.Name == "" {
        c.Name = val
      }
    case "physical id":
      phys = val
    case "core id":
      cores[phys + "/" + val] = true
    case "flags":
      if c.AVX2 || c.AVX512 {
        continue
      }

      for _, f := range strings.Fields(val) {
        switch f {
        case "avx2":
          c.AVX2 = true
        case "avx512f":
          c.AVX512 = true
        }
      }
    case "cpu MHz":
      mhz, _ := strconv.ParseFloat(val, 64)
      if mhz / 1000 > c.GHz {
        c.GHz = mhz / 1000
      }
    }
  }

  c.Cores = len(cores)
  if c.Cores == 0 {
    c.Cores = 1
  }

  khz := atoi64(maxKHz)
  if khz > 0 {
    c.GHz = float64(khz) / 1e6
  }

  return c
}

// diskFree is the free space where dir is, or its nearest existing
// parent; -1 when unknown.
func diskFree(dir string) int64 {
  for d := dir; d != ""; d = filepath.Dir(d) {
    var st syscall.Statfs_t
    err := syscall.Statfs(d, &st)
    if err == nil {
      return int64(st.Bavail) * int64(st.Bsize)
    }

    if d == "/" || d == "." {
      break
    }
  }

  return -1
}

func atoi64(s string) int64 {
  n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
  return n
}

func readFile(path string) string {
  b, err := os.ReadFile(path)
  if err != nil {
    return ""
  }

  return string(b)
}

// describeGPU is one line about g: "RTX 4060 Laptop 8.0 GB (256 GB/s,
// ~45 TFLOPS) · PCIe 4.0 x8".
func describeGPU(g *GPU) string {
  s := g.Name
  if !g.Dedicated {
    return s + " (integrated: shares system RAM)"
  }

  guess := ""
  if !g.Known {
    guess = " assumed"
  }

  s += fmt.Sprintf(
    " %s (%.0f GB/s, ~%.0f TFLOPS%s)",
    gb(g.VRAM), g.Bandwidth, g.TFLOPS, guess,
  )

  if g.PCIeGen > 0 {
    s += fmt.Sprintf(" · PCIe %d.0 x%d", g.PCIeGen, g.Lanes)
  }

  return s
}
