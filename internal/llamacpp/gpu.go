package llamacpp

import (
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"
)

// GPUWarning says why local models would run on the CPU — minutes to
// load, a few tokens a second — when this machine has an NVIDIA GPU
// whose driver isn't usable: no module for the running kernel (a
// kernel update outran the driver), one not loaded, or a module and
// libraries from different driver versions. "" when the driver is
// fine or there's no NVIDIA GPU. Linux only; elsewhere always "".
func GPUWarning() string { return gpuWarning("/") }

// gpuWarning is GPUWarning against a filesystem rooted at root.
func gpuWarning(root string) string {
  at := func(p string) string { return filepath.Join(root, p) }

  if !hasNvidiaGPU(at("sys/bus/pci/devices")) {
    return ""
  }

  release := strings.TrimSpace(readFile(at("proc/sys/kernel/osrelease")))
  loaded := strings.TrimSpace(readFile(at("sys/module/nvidia/version")))
  _, err := os.Stat(at("sys/module/nvidia"))
  if err != nil {
    if hasNvidiaModule(at("lib/modules"), release) {
      return fmt.Sprintf(
        "the NVIDIA driver is installed for kernel %s but not loaded, so local models run on the CPU — reboot (or sudo modprobe nvidia)",
        release,
      )
    }

    msg := fmt.Sprintf("kernel %s has no NVIDIA driver, so local models run on the CPU", release)
    others := kernelsWithNvidia(at("lib/modules"), release)
    if len(others) > 0 && newerKernel(others[len(others) - 1], release) {
      return msg + fmt.Sprintf(" — reboot into %s, which has one", others[len(others) - 1])
    }

    msg += " — update the driver for this kernel (Ubuntu: sudo apt full-upgrade) and reboot"
    if len(others) > 0 {
      msg += fmt.Sprintf(", or pick %s in GRUB, which has one", others[len(others) - 1])
    }

    return msg
  }

  libs := libraryVersion(at)
  if loaded != "" && libs != "" && loaded != libs {
    return fmt.Sprintf(
      "the NVIDIA driver loaded is %s but its libraries are %s, so the GPU can't be used — reboot to load the new driver",
      loaded, libs,
    )
  }

  return ""
}

// hasNvidiaGPU: a display controller (PCI class 0x03xxxx) from
// NVIDIA (vendor 0x10de).
func hasNvidiaGPU(devices string) bool {
  entries, err := os.ReadDir(devices)
  if err != nil {
    return false
  }

  for _, e := range entries {
    dir := filepath.Join(devices, e.Name())
    vendor := strings.TrimSpace(readFile(filepath.Join(dir, "vendor")))
    class := strings.TrimSpace(readFile(filepath.Join(dir, "class")))
    if vendor == "0x10de" && strings.HasPrefix(class, "0x03") {
      return true
    }
  }

  return false
}

// hasNvidiaModule looks where Ubuntu's prebuilt packages
// (kernel/nvidia-*/) and DKMS (updates/dkms/) put nvidia.ko.
func hasNvidiaModule(modules, release string) bool {
  if release == "" {
    return false
  }

  dir := filepath.Join(modules, release)
  for _, pattern := range []string{
    "kernel/nvidia-*/nvidia.ko*", "updates/dkms/nvidia.ko*", "extra/nvidia.ko*",
  } {
    found, _ := filepath.Glob(filepath.Join(dir, pattern))
    if len(found) > 0 {
      return true
    }
  }

  return false
}

// kernelsWithNvidia is every other installed kernel that has the
// module, oldest first.
func kernelsWithNvidia(modules, running string) []string {
  entries, err := os.ReadDir(modules)
  if err != nil {
    return nil
  }

  var out []string
  for _, e := range entries {
    name := e.Name()
    if name != running && hasNvidiaModule(modules, name) {
      out = append(out, name)
    }
  }

  sort.Slice(out, func(i, j int) bool { return newerKernel(out[j], out[i]) })

  return out
}

// newerKernel compares releases number by number, so 6.8.0-147 is
// newer than 6.8.0-99.
func newerKernel(a, b string) bool {
  for a != "" && b != "" {
    na, ra := leadingNumber(a)
    nb, rb := leadingNumber(b)
    if na != nb {
      return na > nb
    }

    a, b = strings.TrimLeft(ra, ".-+_~"), strings.TrimLeft(rb, ".-+_~")
  }

  return a > b
}

// leadingNumber splits s into its leading digits' value (-1 for
// none, then one byte is skipped) and the rest.
func leadingNumber(s string) (int, string) {
  n, i := 0, 0
  for i < len(s) && s[i] >= '0' && s[i] <= '9' {
    n = n * 10 + int(s[i] - '0')
    i++
  }

  if i == 0 {
    return -1, s[1:]
  }

  return n, s[i:]
}

// libraryVersion is the user-space driver's version, from the name
// of its NVML library (libnvidia-ml.so.595.84).
func libraryVersion(at func(string) string) string {
  for _, dir := range []string{ "usr/lib/x86_64-linux-gnu", "usr/lib64", "usr/lib" } {
    found, _ := filepath.Glob(filepath.Join(at(dir), "libnvidia-ml.so.*.*"))
    if len(found) > 0 {
      return strings.TrimPrefix(filepath.Base(found[0]), "libnvidia-ml.so.")
    }
  }

  return ""
}

func readFile(path string) string {
  b, err := os.ReadFile(path)
  if err != nil {
    return ""
  }

  return string(b)
}
