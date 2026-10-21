// The models armin knows by name, so -install takes qwen3.5-9b
// instead of unsloth/Qwen3.5-9B-GGUF: the most downloaded text
// models on Hugging Face from their makers or established
// quantizers, as of October 2026. A table in code ages — anything
// not here still installs by repo or link.

package install

import (
  "fmt"
  "sort"
  "strings"

  "armin/internal/ui"
)

var catalog = map[string]catalogEntry{
  // Qwen and its fine-tunes; qwen35 layers are hybrid: one in four
  // keeps a KV cache.
  "qwen3.8-27b":        { "unsloth/Qwen3.8-27B-GGUF", "27B dense, hybrid attention, 256k" },
  "qwen3.8-flash-next": { "unsloth/Qwen3.8-Flash-Next-GGUF", "177B MoE, 256k" },
  "qwen3.6-35b-a3b":    { "unsloth/Qwen3.6-35B-A3B-GGUF", "35B MoE, 3B active, 256k" },
  "qwen3.5-9b":         { "unsloth/Qwen3.5-9B-GGUF", "9B dense, hybrid attention, 256k" },
  "qwen3.5-4b":         { "unsloth/Qwen3.5-4B-GGUF", "4B dense, hybrid attention, 256k" },
  "qwen3-coder-30b-a3b": { "unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF", "30B MoE, 3B active, code, 256k" },
  "ornith-1.5-9b":      { "ornith-ai/Ornith-1.5-9B-GGUF", "9B dense, hybrid attention, 256k" },
  "ornith-1.5-35b-a3b": { "ornith-ai/Ornith-1.5-35B-A3B-GGUF", "35B MoE, 3B active, 256k" },
  "ornith-1.5-397b":    { "ornith-ai/Ornith-1.5-397B-GGUF", "403B MoE, 256k" },

  // Google
  "gemma-4-e4b":     { "ggml-org/gemma-4-E4B-it-GGUF", "8B (4B effective), 128k" },
  "gemma-4-12b":     { "unsloth/gemma-4-12b-it-GGUF", "12B dense, 256k" },
  "gemma-4-26b-a4b": { "unsloth/gemma-4-26B-A4B-it-GGUF", "25B MoE, 4B active, 256k" },

  // OpenAI, NVIDIA, Mistral, JetBrains
  "gpt-oss-20b":     { "ggml-org/gpt-oss-20b-GGUF", "21B MoE, sliding window, 128k" },
  "gpt-oss-120b":    { "ggml-org/gpt-oss-120b-GGUF", "117B MoE, sliding window, 128k" },
  "nemotron-3.5-lightning-30b-a3b": {
    "ggml-org/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-GGUF",
    "32B MoE, 3B active, Mamba hybrid, 1M",
  },
  "ministral-3-14b":     { "mistralai/Ministral-3-14B-Instruct-2512-GGUF", "14B dense, 256k" },
  "mellum2.1-12b-a2.5b": { "JetBrains/Mellum2.1-12B-A2.5B-Thinking-GGUF", "12B MoE, 2.5B active, code, 128k" },

  // Liquid: convolution layers with a few attention ones, so a small
  // KV cache and good CPU speed.
  "lfm2.5-2.6b":     { "LiquidAI/LFM2.5-2.6B-GGUF", "2.7B liquid hybrid, 128k" },
  "lfm2.5-8b-a1b":   { "LiquidAI/LFM2.5-8B-A1B-GGUF", "8.5B liquid MoE, 1B active, 128k" },
  "lfm2.5-24b-a2b":  { "LiquidAI/LFM2.5-24B-A2B-GGUF", "24B liquid MoE, 2B active, 128k" },

  // Large MoE, for servers or cloud machines
  "glm-5.3-flash":       { "unsloth/GLM-5.3-Flash-GGUF", "321B MoE, 1M" },
  "deepseek-v4.1-flash": { "antirez/deepseek-v4.1-flash-gguf", "755B MoE" },
}

// lookupCatalog finds name in the table, ignoring case.
func lookupCatalog(name string) (catalogEntry, bool) {
  e, ok := catalog[strings.ToLower(name)]
  return e, ok
}

// printCatalog is armin -install list.
func printCatalog() {
  names := make([]string, 0, len(catalog))
  width := 0
  for n := range catalog {
    names = append(names, n)
    if len(n) > width {
      width = len(n)
    }
  }

  sort.Strings(names)
  fmt.Println(ui.Bold("models armin -install knows by name") + ui.Dim(" (add :QUANT to pick one)"))
  for _, n := range names {
    e := catalog[n]
    fmt.Printf("  %-*s  %-38s %s\n", width, n, e.Note, ui.Dim(e.Repo))
  }

  fmt.Println(ui.Dim(
    "\nanything else installs by repo (org/repo[:QUANT]), Hugging Face " +
      "link, or direct .gguf link",
  ))
}
