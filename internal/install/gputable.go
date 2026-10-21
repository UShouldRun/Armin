// The speeds of GPUs armin knows by name: memory bandwidth, which
// limits generating tokens, and effective prefill TFLOPS, which
// limits reading the prompt.
//
// Bandwidth is the spec sheet's. TFLOPS is what llama.cpp's prefill
// reaches on quantized weights (its pp512 results), not the
// marketing peak, and is rough: within a factor of 1.5 or so.

package install

import (
  "strings"
)

var gpuTable = map[string]gpuSpec{
  // NVIDIA GeForce, desktop
  "RTX 5090":          { 1792, 300 },
  "RTX 5080":          { 960, 160 },
  "RTX 5070 TI":       { 896, 125 },
  "RTX 5070":          { 672, 95 },
  "RTX 5060 TI":       { 448, 65 },
  "RTX 5060":          { 448, 55 },
  "RTX 4090":          { 1008, 215 },
  "RTX 4080 SUPER":    { 736, 135 },
  "RTX 4080":          { 717, 125 },
  "RTX 4070 TI SUPER": { 672, 115 },
  "RTX 4070 TI":       { 504, 105 },
  "RTX 4070 SUPER":    { 504, 95 },
  "RTX 4070":          { 504, 75 },
  "RTX 4060 TI":       { 288, 55 },
  "RTX 4060":          { 272, 40 },
  "RTX 3090 TI":       { 1008, 90 },
  "RTX 3090":          { 936, 80 },
  "RTX 3080 TI":       { 912, 75 },
  "RTX 3080":          { 760, 65 },
  "RTX 3070 TI":       { 608, 50 },
  "RTX 3070":          { 448, 45 },
  "RTX 3060 TI":       { 448, 38 },
  "RTX 3060":          { 360, 28 },
  "RTX 3050":          { 224, 18 },
  "RTX 2080 TI":       { 616, 35 },
  "RTX 2080 SUPER":    { 496, 27 },
  "RTX 2080":          { 448, 25 },
  "RTX 2070 SUPER":    { 448, 22 },
  "RTX 2070":          { 448, 20 },
  "RTX 2060 SUPER":    { 448, 17 },
  "RTX 2060":          { 336, 15 },
  "GTX 1660 TI":       { 288, 5 },
  "GTX 1660 SUPER":    { 336, 5 },
  "GTX 1660":          { 192, 4 },
  "GTX 1650":          { 128, 3 },
  "GTX 1080 TI":       { 484, 12 },
  "GTX 1080":          { 320, 8 },
  "GTX 1070 TI":       { 256, 7 },
  "GTX 1070":          { 256, 6 },
  "GTX 1060":          { 192, 4 },
  "GTX 1050 TI":       { 112, 2 },

  // NVIDIA GeForce, laptop
  "RTX 5090 LAPTOP": { 896, 160 },
  "RTX 5080 LAPTOP": { 896, 120 },
  "RTX 5070 LAPTOP": { 384, 60 },
  "RTX 5060 LAPTOP": { 384, 50 },
  "RTX 4090 LAPTOP": { 576, 130 },
  "RTX 4080 LAPTOP": { 432, 95 },
  "RTX 4070 LAPTOP": { 256, 55 },
  "RTX 4060 LAPTOP": { 256, 28 }, // measured: pp512 of a 9B
  "RTX 4050 LAPTOP": { 192, 35 },
  "RTX 3080 LAPTOP": { 448, 45 },
  "RTX 3070 LAPTOP": { 448, 38 },
  "RTX 3060 LAPTOP": { 336, 28 },
  "RTX 3050 LAPTOP": { 192, 14 },

  // NVIDIA workstation and data center
  "RTX PRO 6000":  { 1792, 300 },
  "RTX 6000 ADA":  { 960, 180 },
  "RTX A6000":     { 768, 75 },
  "RTX A5000":     { 768, 55 },
  "RTX A4000":     { 448, 40 },
  "H200":          { 4800, 500 },
  "H100":          { 3350, 450 },
  "H100 PCIE":     { 2000, 350 },
  "A100":          { 1555, 150 },
  "A100 80GB":     { 1935, 150 },
  "A100-SXM4-80GB": { 2039, 150 },
  "L40S":          { 864, 180 },
  "L40":           { 864, 150 },
  "L4":            { 300, 60 },
  "A10":           { 600, 70 },
  "A10G":          { 600, 65 },
  "T4":            { 320, 25 },
  "V100":          { 900, 45 },

  // AMD Radeon (names as pci.ids writes them)
  "RX 9070 XT":  { 640, 60 },
  "RX 9070":     { 640, 50 },
  "RX 9060 XT":  { 320, 30 },
  "RX 7900 XTX": { 960, 60 },
  "RX 7900 XT":  { 800, 50 },
  "RX 7900 GRE": { 576, 40 },
  "RX 7800 XT":  { 624, 35 },
  "RX 7700 XT":  { 432, 28 },
  "RX 7600":     { 288, 18 },
  "RX 6950 XT":  { 576, 22 },
  "RX 6900 XT":  { 512, 20 },
  "RX 6800 XT":  { 512, 18 },
  "RX 6800":     { 512, 15 },
  "RX 6700 XT":  { 384, 12 },
  "RX 6600 XT":  { 256, 9 },
  "RX 6600":     { 224, 8 },
}

// lookupGPU finds name's entry: the longest table name that appears
// in it as whole words, so "RTX 4060 Laptop" beats "RTX 4060" and
// "L4" doesn't match an "L40S".
func lookupGPU(name string) (gpuSpec, bool) {
  up := strings.ToUpper(name)
  best, found := "", false
  for key := range gpuTable {
    if len(key) > len(best) && containsWord(up, key) {
      best, found = key, true
    }
  }

  return gpuTable[best], found
}

// containsWord: key occurs in s with no letter or digit glued to
// either side.
func containsWord(s, key string) bool {
  for i := 0; ; {
    j := strings.Index(s[i:], key)
    if j < 0 {
      return false
    }

    start, end := i + j, i + j + len(key)
    if (start == 0 || !alnum(s[start - 1])) && (end == len(s) || !alnum(s[end])) {
      return true
    }

    i = start + 1
  }
}

func alnum(b byte) bool {
  return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}
