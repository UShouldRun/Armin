package install

import (
  "strings"
  "testing"
)

// dense is an 8B-like dense model at about 4.5 bits.
func dense(total int64) Model {
  return Model{
    Arch: "llama", Layers: 32, Context: 131072, Heads: 32, HeadDim: 128,
    AttnLayers: 32, KVFull: 32 * 8 * 256 * kvBytesQ8,
    Total: total, Embed: total / 16, Output: total / 16,
    NonLayer: total / 8, Params: total * 16 / 9, EmbedParams: total / 9,
  }
}

// moe is a 30B-A3B-like model: 128 experts, 8 active.
func moe(total int64) Model {
  m := dense(total)
  m.Layers, m.AttnLayers, m.ExpertCount, m.ExpertUsed = 48, 48, 128, 8
  m.KVFull = 48 * 4 * 256 * kvBytesQ8
  m.Experts, m.ExpertParams = total * 85 / 100, m.Params * 85 / 100

  return m
}

func laptop() Machine {
  g := GPU{
    Name: "RTX 4060 Laptop", Dedicated: true, Known: true,
    VRAM: 8 * gib, VRAMFree: 8 * gib, Bandwidth: 256, TFLOPS: 28,
    PCIeGen: 4, Lanes: 8,
  }

  hw := Machine{
    GPUs:     []GPU{ g },
    RAM:      RAM{ Total: 16 * gib, Available: 12 * gib, Type: "DDR5", Speed: 5600, Channels: 2, Bandwidth: 89.6 },
    CPU:      CPU{ Cores: 8, GHz: 5, AVX2: true, AVX512: true },
    DiskFree: 100 * gib,
  }

  hw.GPU = &hw.GPUs[0]

  return hw
}

func hasCause(p *Plan, c Cause) bool {
  for _, f := range p.Findings {
    if f.Cause == c {
      return true
    }
  }

  return false
}

func TestDecideFitsGPU(t *testing.T) {
  m, hw := dense(4 * gib), laptop()
  p := decide(&m, &hw, 4 * gib)
  if p.Place != PlaceGPU || p.Verdict != VerdictFine {
    t.Fatalf("place %d verdict %s: %+v", p.Place, p.Verdict, p.Findings)
  }

  // The largest window whose cache still fits beside the weights.
  if m.GPUWeights() + m.KVBytes(p.Context) > hw.GPU.VRAMFree - vramReserve {
    t.Errorf("context %d doesn't fit", p.Context)
  }

  if p.Context < minContext || strings.Join(p.Args[:3], " ") != "--jinja -ngl 999" {
    t.Errorf("context %d args %v", p.Context, p.Args)
  }

  if p.GenCause != CauseVRAMBus || p.PreCause != CauseGPUCompute {
    t.Errorf("limits: %s, %s", p.GenCause, p.PreCause)
  }
}

func TestDecideMoEExpertsInRAM(t *testing.T) {
  m, hw := moe(15 * gib), laptop()
  p := decide(&m, &hw, 0)
  if p.Place != PlaceExperts || p.CPUMoE <= 0 || p.CPUMoE >= m.Layers {
    t.Fatalf("place %d, %d layers' experts in RAM", p.Place, p.CPUMoE)
  }

  if p.VRAMUse > hw.GPU.VRAMFree - vramReserve {
    t.Errorf("VRAM %s over budget", gb(p.VRAMUse))
  }

  if p.GenCause != CauseRAMBus || !hasCause(&p, CauseRAMBus) {
    t.Errorf("generation limited by %s", p.GenCause)
  }

  if p.Args[len(p.Args) - 2] != "--n-cpu-moe" {
    t.Errorf("args %v", p.Args)
  }
}

func TestDecideDenseSplit(t *testing.T) {
  m, hw := dense(12 * gib), laptop()
  p := decide(&m, &hw, 0)
  if p.Place != PlaceSplit || p.GPULayers <= 0 || p.GPULayers >= m.Layers {
    t.Fatalf("place %d, %d layers on the GPU", p.Place, p.GPULayers)
  }

  if p.Verdict < VerdictSlow || p.GenCause != CauseRAMBus {
    t.Errorf("verdict %s, generation limited by %s", p.Verdict, p.GenCause)
  }

  if len(p.Fixes) == 0 {
    t.Error("no fixes for a split dense model")
  }
}

func TestDecideWontLoad(t *testing.T) {
  m, hw := dense(30 * gib), laptop()
  p := decide(&m, &hw, 0)
  if p.Verdict != VerdictWontLoad || !hasCause(&p, CauseMemory) {
    t.Errorf("30 GB on 8+16: %s %+v", p.Verdict, p.Findings)
  }

  m = dense(4 * gib)
  hw.DiskFree = gib
  p = decide(&m, &hw, 4 * gib)
  if p.Verdict != VerdictWontLoad || !hasCause(&p, CauseDisk) {
    t.Errorf("no disk: %s %+v", p.Verdict, p.Findings)
  }

  m.NoRuntime = "a diffusion model"
  p = decide(&m, &hw, 0)
  if p.Verdict != VerdictWontLoad || !hasCause(&p, CauseRuntime) {
    t.Errorf("no runtime: %s %+v", p.Verdict, p.Findings)
  }
}

func TestDecideFreeRAM(t *testing.T) {
  m, hw := moe(15 * gib), laptop()
  hw.RAM.Available = 4 * gib
  p := decide(&m, &hw, 0)
  if !hasCause(&p, CauseFreeRAM) || p.Verdict < VerdictSlow {
    t.Errorf("little free RAM: %s %+v", p.Verdict, p.Findings)
  }
}

func TestDecideCPUOnly(t *testing.T) {
  m, hw := dense(4 * gib), laptop()
  hw.GPUs, hw.GPU = nil, nil
  p := decide(&m, &hw, 0)
  if p.Place != PlaceCPU || p.PreCause != CauseCPUCompute || p.GenCause != CauseRAMBus {
    t.Fatalf("place %d, limits %s, %s", p.Place, p.GenCause, p.PreCause)
  }

  if p.Context != minContext || p.Args[2] != "0" {
    t.Errorf("context %d args %v", p.Context, p.Args)
  }

  // Slower RAM is slower generation.
  slow := hw
  slow.RAM.Bandwidth = 25.6
  q := decide(&m, &slow, 0)
  if q.GenEmpty >= p.GenEmpty {
    t.Errorf("DDR4 single channel %.1f tok/s ≥ DDR5 dual %.1f", q.GenEmpty, p.GenEmpty)
  }
}

func TestDecideDriverProblem(t *testing.T) {
  m, hw := dense(4 * gib), laptop()
  hw.GPUs[0].Problem = "the driver isn't loaded"
  hw.GPU = nil
  p := decide(&m, &hw, 0)
  if !hasCause(&p, CauseDriver) || p.Place != PlaceCPU {
    t.Errorf("driver problem: place %d %+v", p.Place, p.Findings)
  }
}

func TestScaledAlternatives(t *testing.T) {
  m := moe(15 * gib)
  s := m.scaled(0.5)
  if s.Total != m.Total / 2 || s.Experts != m.Experts / 2 || s.Params != m.Params {
    t.Errorf("scaled: %+v", s)
  }
}

func TestBestGroup(t *testing.T) {
  m, hw := dense(5 * gib), laptop()
  hw.RAM.Total, hw.RAM.Available = 64 * gib, 60 * gib
  src := &source{ Pick: 2, Groups: []quantGroup{
    { Label: "Q2_K", Size: 3 * gib },
    { Label: "Q3_K_M", Size: 4 * gib },
    { Label: "Q4_K_M", Size: 5 * gib },
    { Label: "Q8_0", Size: 9 * gib },
    { Label: "F16", Size: 16 * gib },
  } }

  // Q8_0 and F16 run (split, slow or fine), but Q4_K_M is the
  // largest that fits all in VRAM.
  got := bestGroup(&m, &hw, src)
  if src.Groups[got].Label != "Q4_K_M" {
    t.Errorf("picked %s", src.Groups[got].Label)
  }

  if looksLikeHash("Qwen3.5-9B") || !looksLikeHash("feb5e04da4910dd56d33f0cd03747ff298c1e801") {
    t.Error("looksLikeHash")
  }
}
