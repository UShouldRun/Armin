// The decision tree: where a model's weights and KV cache go on this
// machine, how fast it then generates and reads prompts, and what
// limits each, in words and numbers.
//
// Generating a token reads every active weight and the whole KV
// cache once, so it's bound by memory bandwidth: time per token is
// the bytes on each side over that side's bandwidth. Reading a
// prompt is matrix multiplication, bound by compute (FLOPs), plus,
// when weights live in RAM, copying them to the GPU over PCIe once
// per batch. Estimates are ceilings scaled by the efficiencies
// llama.cpp typically reaches; a benchmark after the download checks
// them.

package install

import (
  "fmt"
  "math"
)

const (
  VerdictFine Verdict = iota
  VerdictSlow
  VerdictTooSlow
  VerdictWontLoad
)

const (
  CauseNone Cause = iota
  CauseDisk
  CauseMemory
  CauseContext
  CauseRuntime
  CauseDriver
  CauseFreeRAM
  CauseVRAMBus
  CauseRAMBus
  CauseKVReads
  CauseGPUCompute
  CauseCPUCompute
  CausePCIe
  CauseNoAVX2
)

const (
  PlaceGPU     Place = iota // everything in VRAM
  PlaceExperts              // MoE: experts of some layers in RAM
  PlaceSplit                // the first layers in VRAM, the rest in RAM
  PlaceCPU                  // all in RAM
)

var causeNames = [...]string{
  CauseNone:       "",
  CauseDisk:       "disk space",
  CauseMemory:     "memory",
  CauseContext:    "context memory",
  CauseRuntime:    "runtime",
  CauseDriver:     "GPU driver",
  CauseFreeRAM:    "free RAM",
  CauseVRAMBus:    "GPU memory bus",
  CauseRAMBus:     "system RAM bus",
  CauseKVReads:    "context memory reads",
  CauseGPUCompute: "GPU compute",
  CauseCPUCompute: "CPU compute",
  CausePCIe:       "PCIe bus",
  CauseNoAVX2:     "CPU without AVX2",
}

func (c Cause) String() string { return causeNames[c] }

var verdictNames = [...]string{ "fine", "slow", "too slow", "won't load" }

func (v Verdict) String() string { return verdictNames[v] }

const (
  vramReserve = 1 * gib   // CUDA context and compute buffers
  ramReserve  = gib / 2   // headroom for the rest of the system
  decodeEff   = 0.75      // of VRAM bandwidth reached generating
  ramEff      = 0.6       // of RAM bandwidth: CPU threads reach less
  moeEff      = 0.8       // MoE: routing and scattered expert reads
  cpuEff      = 0.8       // of the CPU's peak reached on prefill
  ubatch      = 512       // llama.cpp's prefill batch
  minContext  = 32768     // below this an agent runs out of room
  agentPrompt = 30000     // tokens: a long agent context, from scratch

  // Thresholds, tokens/s generating and seconds to read agentPrompt.
  genUnusable = 5.0
  genSlow     = 15.0
  preSlow     = 60.0
  preTooSlow  = 300.0
)

// contexts are the windows tried, largest first.
var contexts = []int{ 131072, 98304, 65536, 32768, 16384, 8192 }

// decide runs the tree for model m on machine hw. diskNeed is what
// still has to be downloaded (0 for a file already on disk).
func decide(m *Model, hw *Machine, diskNeed int64) Plan {
  var p Plan
  if m.NoRuntime != "" {
    p.add(VerdictWontLoad, CauseRuntime, m.NoRuntime)
    return p
  }

  if diskNeed > 0 && hw.DiskFree >= 0 && diskNeed > hw.DiskFree {
    p.add(VerdictWontLoad, CauseDisk, fmt.Sprintf(
      "the download is %s and only %s is free there",
      gb(diskNeed), gb(hw.DiskFree),
    ))
  }

  for _, g := range hw.GPUs {
    if g.Problem != "" {
      p.add(VerdictSlow, CauseDriver, g.Problem)
    }
  }

  p.place(m, hw)
  if p.Verdict == VerdictWontLoad {
    return p
  }

  p.estimate(m, hw)
  p.judge(m, hw)

  return p
}

// add records a finding and raises the verdict to its level.
func (p *Plan) add(level Verdict, c Cause, text string) {
  p.Findings = append(p.Findings, Finding{ Level: level, Cause: c, Text: text })
  if level > p.Verdict {
    p.Verdict = level
  }
}

// place picks the window and where the weights go:
//
//   all of it fits in VRAM at ≥ 32k tokens   → all on the GPU, at the
//                                              largest window that fits
//   MoE whose shared part fits at 32k        → experts of the fewest
//                                              layers in RAM
//   dense, or MoE whose shared part doesn't  → as many layers in VRAM
//                                              as fit, the rest in RAM
//   no usable dedicated GPU                  → all in RAM
//
// and then whether RAM holds its share: more than the machine has
// won't load; more than is free now pushes other programs out.
func (p *Plan) place(m *Model, hw *Machine) {
  budget := int64(0)
  if hw.GPU != nil {
    budget = hw.GPU.VRAMFree - vramReserve
  }

  window := func(fits func(ctx int) bool) int {
    for _, c := range contexts {
      if c <= m.Context && c >= minContext && fits(c) {
        return c
      }
    }

    return 0
  }

  agentCtx := minContext
  if m.Context > 0 && m.Context < agentCtx {
    agentCtx = m.Context
  }

  ctx := window(func(c int) bool { return m.GPUWeights() + m.KVBytes(c) <= budget })
  switch {
  case hw.GPU == nil:
    p.Place, p.Context = PlaceCPU, agentCtx
  case ctx > 0:
    p.Place, p.Context = PlaceGPU, ctx
  case m.Experts > 0:
    // A larger window here would push more experts into RAM and
    // slow every token, so it stays at what an agent needs.
    shared := m.GPUWeights() - m.Experts
    if shared + m.KVBytes(agentCtx) <= budget {
      p.Place, p.Context = PlaceExperts, agentCtx
      break
    }

    fallthrough
  default:
    p.Place, p.Context = PlaceSplit, agentCtx
  }

  kv := m.KVBytes(p.Context)
  layers := int64(m.Layers)
  if layers == 0 {
    layers = 1
  }

  perLayer := (m.Total - m.NonLayer) / layers
  switch p.Place {
  case PlaceGPU:
    p.GPULayers = m.Layers
    p.VRAMUse = m.GPUWeights() + kv
    p.RAMUse = m.Total - m.GPUWeights()
  case PlaceExperts:
    expertsPerLayer := m.Experts / layers
    over := m.GPUWeights() + kv - budget
    n := int((over + expertsPerLayer - 1) / expertsPerLayer)
    if n > m.Layers {
      n = m.Layers
    }

    p.GPULayers, p.CPUMoE = m.Layers, n
    p.RAMUse = int64(n) * expertsPerLayer + m.Total - m.GPUWeights()
    p.VRAMUse = m.Total + kv - p.RAMUse
  case PlaceSplit:
    kvPerLayer := kv / layers
    output := m.GPUWeights() - (m.Total - m.NonLayer) // output + norms
    n := (budget - output) / (perLayer + kvPerLayer)
    if n < 0 {
      n = 0
    }

    if n >= layers {
      n = layers - 1
    }

    p.GPULayers = int(n)
    if n == 0 {
      p.Place = PlaceCPU
      break
    }

    p.VRAMUse = output + n * (perLayer + kvPerLayer)
    p.RAMUse = m.Total + kv - p.VRAMUse
  }

  if p.Place == PlaceCPU {
    p.GPULayers, p.VRAMUse = 0, 0
    p.RAMUse = m.Total + kv
  }

  p.Args = []string{ "--jinja", "-ngl", fmt.Sprint(p.GPULayers), "-c", fmt.Sprint(p.Context) }
  if p.Place == PlaceGPU {
    p.Args[2] = "999"
  }

  if p.CPUMoE > 0 {
    p.Args = append(p.Args, "--n-cpu-moe", fmt.Sprint(p.CPUMoE))
  }

  need := p.RAMUse + ramReserve
  switch {
  case need > hw.RAM.Total:
    besides := ""
    if p.VRAMUse > 0 {
      besides = " besides " + gb(p.VRAMUse) + " in VRAM"
    }

    p.add(VerdictWontLoad, CauseMemory, fmt.Sprintf(
      "needs %s of system RAM%s and the machine has %s: the weights "+
        "would stream from disk every token",
      gb(need), besides, gb(hw.RAM.Total),
    ))
  case need > hw.RAM.Available:
    p.add(VerdictSlow, CauseFreeRAM, fmt.Sprintf(
      "needs %s of RAM and %s is free now: other programs get pushed "+
        "to swap (%s free) — close some before loading it",
      gb(need), gb(hw.RAM.Available), gb(hw.RAM.SwapFree),
    ))
  }
}

// estimate fills the speeds: generation at an empty and a full
// window, and prefill, each with the side that limits it.
func (p *Plan) estimate(m *Model, hw *Machine) {
  ramBW := hw.RAM.Bandwidth * 1e9 * ramEff
  gpuBW := ramBW
  if hw.GPU != nil {
    gpuBW = hw.GPU.Bandwidth * 1e9 * decodeEff
  }

  read := float64(m.ReadPerToken())
  layers := float64(m.Layers)
  if layers == 0 {
    layers = 1
  }

  // Weight bytes read per token on each side, and the share of the
  // KV cache that lives in RAM.
  var gpuRead, cpuRead, kvRAM float64
  switch p.Place {
  case PlaceGPU:
    gpuRead = read
  case PlaceExperts:
    cpuRead = float64(m.Experts) / layers * float64(p.CPUMoE) * m.activeShare()
    gpuRead = read - cpuRead
  case PlaceSplit:
    perLayer := (read - float64(m.NonLayer - m.Embed)) / layers
    cpuRead = perLayer * (layers - float64(p.GPULayers))
    gpuRead = read - cpuRead
    kvRAM = 1 - float64(p.GPULayers) / layers
  case PlaceCPU:
    cpuRead, kvRAM = read, 1
  }

  at := func(ctx int) (float64, float64, float64) {
    kv := float64(m.KVBytes(ctx))
    g := gpuRead / gpuBW + kv * (1 - kvRAM) / gpuBW
    c := cpuRead / ramBW + kv * kvRAM / ramBW
    k := kv * (1 - kvRAM) / gpuBW + kv * kvRAM / ramBW

    return g, c, k
  }

  eff := 1.0
  if m.Experts > 0 {
    eff = moeEff
  }

  g0, c0, _ := at(0)
  g1, c1, k1 := at(p.Context)
  p.GenEmpty, p.GenFull = eff / (g0 + c0), eff / (g1 + c1)
  switch {
  case p.Place == PlaceCPU:
    p.GenCause = CauseRAMBus
  case c0 > g0:
    p.GenCause = CauseRAMBus
  default:
    p.GenCause = CauseVRAMBus
  }

  if k1 > (g1 + c1) / 2 {
    p.GenCause = CauseKVReads
  }

  // Prefill: FLOPs per token at the average depth of agentPrompt,
  // over the compute of whichever side multiplies.
  attn := 4 * float64(m.AttnLayers) * float64(m.Heads * m.HeadDim) * agentPrompt / 2
  flops := 2 * float64(m.ActiveParams()) + attn
  if p.Place == PlaceCPU {
    p.Prefill = cpuFLOPS(&hw.CPU) / flops
    p.PreCause = CauseCPUCompute
    return
  }

  compute := ubatch * flops / (hw.GPU.TFLOPS * 1e12)
  copyTime := 0.0
  pcie := PCIeBandwidth(hw.GPU.PCIeGen, hw.GPU.Lanes) * 1e9
  if pcie > 0 && p.RAMUse > 0 && p.Place != PlaceGPU {
    // llama.cpp copies the RAM-resident weights to the GPU for every
    // batch; the embedding table never goes.
    copyTime = float64(p.RAMUse - m.Embed) / pcie
  }

  p.Prefill = ubatch / (compute + copyTime)
  p.PreCause = CauseGPUCompute
  if copyTime > compute {
    p.PreCause = CausePCIe
  }
}

// cpuFLOPS is the CPU's effective multiply rate: per core per cycle,
// llama.cpp's quantized dot products do about 64 operations with
// AVX-512, 32 with AVX2, 8 with neither.
func cpuFLOPS(c *CPU) float64 {
  perCycle := 8.0
  switch {
  case c.AVX512:
    perCycle = 64
  case c.AVX2:
    perCycle = 32
  }

  ghz := c.GHz
  if ghz == 0 {
    ghz = 2.5
  }

  return float64(c.Cores) * ghz * 1e9 * 0.85 * perCycle * cpuEff
}

// judge turns the speeds into findings that say what limits each,
// with the numbers behind it, and suggests fixes.
func (p *Plan) judge(m *Model, hw *Machine) {
  level := func(tps, slow float64) Verdict {
    switch {
    case tps < genUnusable:
      return VerdictTooSlow
    case tps < slow:
      return VerdictSlow
    }

    return VerdictFine
  }

  ram := ramName(&hw.RAM)
  gen := level(p.GenEmpty, genSlow)
  switch p.Place {
  case PlaceGPU:
    p.add(gen, CauseVRAMBus, fmt.Sprintf(
      "all %s of weights and the context fit in VRAM; each token reads "+
        "%s at %.0f GB/s",
      gb(m.GPUWeights()), gb(m.ReadPerToken()), hw.GPU.Bandwidth,
    ))
  case PlaceExperts:
    p.add(gen, CauseRAMBus, fmt.Sprintf(
      "the experts of %d of %d layers (%s) don't fit in VRAM and live "+
        "in RAM; each token reads its active experts there from %s "+
        "(%.0f GB/s vs the GPU's %.0f)",
      p.CPUMoE, m.Layers, gb(p.RAMUse - m.Embed), ram,
      hw.RAM.Bandwidth, hw.GPU.Bandwidth,
    ))
  case PlaceSplit:
    p.add(gen, CauseRAMBus, fmt.Sprintf(
      "%s of weights don't fit in VRAM: %d of %d layers run from %s, "+
        "read every token at %.0f GB/s vs the GPU's %.0f",
      gb(p.RAMUse - m.Embed), m.Layers - p.GPULayers, m.Layers, ram,
      hw.RAM.Bandwidth, hw.GPU.Bandwidth,
    ))
  case PlaceCPU:
    why := "no dedicated GPU"
    if hw.GPU != nil {
      why = "not even one layer fits in VRAM"
    }

    p.add(gen, CauseRAMBus, fmt.Sprintf(
      "%s: every token reads %s from %s at %.0f GB/s",
      why, gb(m.ReadPerToken()), ram, hw.RAM.Bandwidth,
    ))
  }

  if p.GenFull < p.GenEmpty * 0.6 {
    full := VerdictFine
    if p.GenFull < genUnusable {
      full = VerdictSlow
    }

    p.add(full, CauseKVReads, fmt.Sprintf(
      "at %dk tokens the context memory (%s) is read every token too: "+
        "%.0f tok/s, down from %.0f",
      p.Context / 1024, gb(m.KVBytes(p.Context)), p.GenFull, p.GenEmpty,
    ))
  }

  secs := agentPrompt / p.Prefill
  pre := VerdictFine
  switch {
  case secs > preTooSlow:
    pre = VerdictTooSlow
  case secs > preSlow:
    pre = VerdictSlow
  }

  switch p.PreCause {
  case CauseCPUCompute:
    p.add(pre, CauseCPUCompute, fmt.Sprintf(
      "the prompt is multiplied on the CPU: %d cores at %.1f GHz, "+
        "%s — %s for a %dk-token context",
      hw.CPU.Cores, hw.CPU.GHz, simdName(&hw.CPU), duration(secs),
      agentPrompt / 1000,
    ))
  case CausePCIe:
    p.add(pre, CausePCIe, fmt.Sprintf(
      "every %d-token batch copies the %s in RAM to the GPU over "+
        "PCIe %d.0 x%d (%.0f GB/s) — %s for a %dk-token context",
      ubatch, gb(p.RAMUse - m.Embed), hw.GPU.PCIeGen, hw.GPU.Lanes,
      PCIeBandwidth(hw.GPU.PCIeGen, hw.GPU.Lanes), duration(secs),
      agentPrompt / 1000,
    ))
  default:
    p.add(pre, CauseGPUCompute, fmt.Sprintf(
      "~%.0f TFLOPS reads a %dk-token context in %s",
      hw.GPU.TFLOPS, agentPrompt / 1000, duration(secs),
    ))
  }

  if p.Place != PlaceGPU && !hw.CPU.AVX2 {
    p.add(VerdictSlow, CauseNoAVX2, "the CPU has no AVX2, so the "+
      "layers it runs are several times slower than on a newer one")
  }

  p.suggest(m, hw)
}

// suggest names fixes for what judge found.
func (p *Plan) suggest(m *Model, hw *Machine) {
  if p.Verdict == VerdictFine {
    return
  }

  if hw.GPU != nil && p.Place != PlaceGPU {
    fits := hw.GPU.VRAMFree - vramReserve - m.KVBytes(minContext)
    if fits > 0 {
      p.Fixes = append(p.Fixes, fmt.Sprintf(
        "a quantization of %s or less fits entirely in VRAM at 32k tokens",
        gb(fits),
      ))
    }
  }

  if p.PreCause == CausePCIe {
    p.Fixes = append(p.Fixes, "-ub 2048 -b 2048 copies the weights a "+
      "quarter as often while reading prompts, for more VRAM")
  }

  if p.Place == PlaceSplit && m.Experts == 0 {
    p.Fixes = append(p.Fixes, "a MoE model of similar size: only its "+
      "active experts are read per token, so RAM hurts far less")
  }
}

// ramName is "DDR5-5600 2ch" or "RAM" when unknown.
func ramName(r *RAM) string {
  if r.Type == "" {
    return "system RAM"
  }

  return fmt.Sprintf("%s-%d %dch", r.Type, r.Speed, r.Channels)
}

func simdName(c *CPU) string {
  switch {
  case c.AVX512:
    return "AVX-512"
  case c.AVX2:
    return "AVX2"
  }

  return "no AVX2"
}

// gb is bytes as "19.8 GB" (GiB, as llama.cpp prints them).
func gb(b int64) string {
  return fmt.Sprintf("%.1f GB", float64(b) / gib)
}

// duration is "25 s", "4.2 min" or "1.3 h".
func duration(secs float64) string {
  switch {
  case math.IsInf(secs, 0) || math.IsNaN(secs):
    return "forever"
  case secs < 90:
    return fmt.Sprintf("%.0f s", secs)
  case secs < 5400:
    return fmt.Sprintf("%.1f min", secs / 60)
  }

  return fmt.Sprintf("%.1f h", secs / 3600)
}
