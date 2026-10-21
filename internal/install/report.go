// Printing what the tree found: the model, the machine, the plan,
// the verdict with each limit and its numbers, the fixes, and how
// the repo's other quantizations would do.

package install

import (
  "fmt"
  "strings"

  "armin/internal/ui"
)

func printModel(m *Model, size int64) {
  trained := ""
  if m.Context > 0 {
    trained = fmt.Sprintf(" · trained to %dk", m.Context / 1024)
  }

  fmt.Printf(
    "%s · %s · %s · %s · %d layers%s\n",
    ui.Bold(firstNonEmpty(m.Name, m.Arch)), m.Quant, gb(size), m.Kind,
    m.Layers, trained,
  )
}

func printMachine(hw *Machine, dir string) {
  fmt.Println(ui.Bold("machine"))
  if len(hw.GPUs) == 0 {
    fmt.Printf("  %s  none found\n", ui.Dim("GPU "))
  }

  for i := range hw.GPUs {
    label := "    "
    if i == 0 {
      label = "GPU "
    }

    fmt.Printf("  %s  %s\n", ui.Dim(label), describeGPU(&hw.GPUs[i]))
  }

  r := &hw.RAM
  fmt.Printf(
    "  %s  %s, %s free · %s (%.0f GB/s, %s)\n",
    ui.Dim("RAM "), gb(r.Total), gb(r.Available), ramName(r),
    r.Bandwidth, r.Source,
  )
  fmt.Printf(
    "  %s  %s · %d cores %.1f GHz · %s\n",
    ui.Dim("CPU "), hw.CPU.Name, hw.CPU.Cores, hw.CPU.GHz, simdName(&hw.CPU),
  )

  if hw.DiskFree >= 0 {
    fmt.Printf("  %s  %s free in %s\n", ui.Dim("disk"), gb(hw.DiskFree), dir)
  }

  for _, n := range hw.Notes {
    fmt.Println(ui.Dim("  note: " + n))
  }
}

func printPlan(m *Model, p *Plan) {
  fmt.Println()
  if p.Verdict != VerdictWontLoad || p.Context > 0 {
    fmt.Printf("%s %s · %dk context\n", ui.Bold("plan"), placeText(m, p), p.Context / 1024)
    fmt.Printf(
      "  VRAM %s · RAM %s · args: %s\n",
      gb(p.VRAMUse), gb(p.RAMUse), strings.Join(p.Args, " "),
    )
    fmt.Println()
  }

  fmt.Printf("%s %s\n", ui.Bold("verdict"), colorVerdict(p.Verdict, 0))
  if p.Verdict != VerdictWontLoad {
    fmt.Printf(
      "  generation  ~%.0f tok/s (%.0f at %dk)   limited by %s\n",
      p.GenEmpty, p.GenFull, p.Context / 1024, p.GenCause,
    )
    fmt.Printf(
      "  prefill     ~%.0f tok/s, %s per %dk tokens   limited by %s\n",
      p.Prefill, duration(agentPrompt / p.Prefill), agentPrompt / 1000, p.PreCause,
    )
  }

  for _, f := range p.Findings {
    fmt.Printf("  %s %s: %s\n", mark(f.Level), ui.Bold(f.Cause.String()), f.Text)
  }

  if len(p.Fixes) > 0 {
    fmt.Println(ui.Bold("fixes"))
    for _, f := range p.Fixes {
      fmt.Println("  · " + f)
    }
  }
}

func placeText(m *Model, p *Plan) string {
  switch p.Place {
  case PlaceGPU:
    return "all on the GPU"
  case PlaceExperts:
    return fmt.Sprintf(
      "on the GPU, experts of %d of %d layers in RAM", p.CPUMoE, m.Layers,
    )
  case PlaceSplit:
    return fmt.Sprintf(
      "%d of %d layers on the GPU, %d in RAM",
      p.GPULayers, m.Layers, m.Layers - p.GPULayers,
    )
  }

  return "all on the CPU, in RAM"
}

func printAlternatives(alts []alternative) {
  if len(alts) < 2 {
    return
  }

  fmt.Println()
  fmt.Println(ui.Bold("the repo's quantizations") + ui.Dim(" (estimated from this one's shape)"))
  for _, a := range alts {
    speed := ""
    if a.plan.Verdict != VerdictWontLoad {
      speed = fmt.Sprintf("~%.0f tok/s · %s per %dk", a.plan.GenEmpty,
        duration(agentPrompt / a.plan.Prefill), agentPrompt / 1000)
    }

    pick := ""
    if a.picked {
      pick = ui.Cyan("  ← this one")
    }

    fmt.Printf(
      "  %-12s %8s  %s %s%s\n",
      a.label, gb(a.size), colorVerdict(a.plan.Verdict, 10), speed, pick,
    )
  }
}

// colorVerdict pads to width before coloring, so columns line up.
func colorVerdict(v Verdict, width int) string {
  s := fmt.Sprintf("%-*s", width, v)
  switch v {
  case VerdictFine:
    return ui.Green(s)
  case VerdictSlow:
    return ui.Yellow(s)
  }

  return ui.Red(s)
}

func mark(v Verdict) string {
  switch v {
  case VerdictFine:
    return ui.Green("✓")
  case VerdictSlow:
    return ui.Yellow("!")
  }

  return ui.Red("✗")
}

func firstNonEmpty(a, b string) string {
  if a != "" {
    return a
  }

  return b
}
