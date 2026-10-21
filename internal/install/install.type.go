// The types of package install: the model as its header describes
// it, the machine as Linux describes it, and the plan that matches
// one to the other.

package install

import (
  "bufio"
  "context"
  "io"
  "net/http"
  "time"
)

// Model is a .gguf as its header describes it: its shape, and how
// many of its bytes play each role, which is what decides where they
// can live and how many are read per token.
type Model struct {
  Arch, Name, Quant string
  Kind              string // "dense", "MoE 64 experts, 8 active", …

  Layers, Context         int // blocks; trained window
  ExpertCount, ExpertUsed int
  Heads, HeadDim          int // attention, for prefill's FLOPs
  AttnLayers, SWALayers   int // layers that keep a KV cache
  Window                  int // sliding-window size, 0 for none

  // KV cache bytes per cached token (q8_0), summed over full
  // attention layers and over sliding-window ones.
  KVFull, KVSWA float64

  Total, Experts, Embed, Output, NonLayer int64 // bytes
  Params, ExpertParams, EmbedParams       int64 // weights

  NoRuntime string // why llama-server can't serve it, "" if it can
}

// Vendor is a GPU's maker, from its PCI vendor id.
type Vendor uint8

// GPU is one display controller and what it can do for a model.
type GPU struct {
  Name             string
  Vendor           Vendor
  Dedicated, Known bool    // own memory; found in the GPU table
  VRAM, VRAMFree   int64   // bytes
  Bandwidth        float64 // GB/s
  TFLOPS           float64 // effective for llama.cpp's prefill
  PCIeGen, Lanes   int
  Problem          string  // driver trouble: the GPU can't be used
}

// RAM is system memory: how much, and how fast its bus is.
type RAM struct {
  Total, Available, SwapFree int64 // bytes
  Type                       string // "DDR5", "LPDDR5", …
  Speed, Channels            int    // MT/s; 64-bit channels
  Bandwidth                  float64 // GB/s
  Source                     string // where Type/Speed came from
}

// CPU is the processor, for prefill when weights live in RAM.
type CPU struct {
  Name         string
  Cores        int
  GHz          float64
  AVX2, AVX512 bool
}

// Machine is everything the decision tree reads about the computer.
type Machine struct {
  GPUs     []GPU
  GPU      *GPU // the dedicated one models run on, nil for none
  RAM      RAM
  CPU      CPU
  DiskFree int64 // where the model will be saved
  Notes    []string
}

// Verdict is how well a model runs, worst last.
type Verdict uint8

// Cause is what limits a model on a machine.
type Cause uint8

// Place is where the weights go.
type Place uint8

// Finding is one reason behind a verdict: its level, what limits the
// model, and the numbers that show it.
type Finding struct {
  Level Verdict
  Cause Cause
  Text  string
}

// Plan is the decision tree's answer for one model on one machine.
type Plan struct {
  Context            int
  Place              Place
  GPULayers, CPUMoE  int   // -ngl; --n-cpu-moe
  VRAMUse, RAMUse    int64 // bytes
  GenEmpty, GenFull  float64 // tokens/s at an empty and a full window
  Prefill            float64 // tokens/s
  GenCause, PreCause Cause   // what limits each
  Args               []string
  Verdict            Verdict
  Findings           []Finding
  Fixes              []string
}

// hubFile is one file in a Hugging Face repo.
type hubFile struct {
  Path   string
  Size   int64
  SHA256 string
}

// quantGroup is one quantization in a repo: its files (several when
// the .gguf is split) and their total size.
type quantGroup struct {
  Label string
  Files []hubFile
  Size  int64
}

// source is what -install was given, resolved: a file on disk, or a
// quantization in a Hugging Face repo.
type source struct {
  Local  string // a .gguf path, or ""
  URL    string // a direct link to a .gguf, or ""
  Repo   string
  Rev    string // the repo's branch or commit
  Quant  string // what the user asked for, maybe ""
  File   string // the file the user linked, maybe ""
  Groups []quantGroup
  Pick   int // index into Groups
}

// rangeReader reads a remote file front to back in Range requests,
// so a header can be parsed without downloading the weights.
type rangeReader struct {
  ctx        context.Context
  client     *http.Client
  url, token string
  off, size  int64
  buf        []byte
  pos        int
}

// Request is what Run needs from the command line.
type Request struct {
  Model   string // org/repo[:QUANT], org/repo/file.gguf, a URL or a path
  To      string // download folder, "" to pick one
  Yes     bool   // don't ask
  Models  map[string]Existing // config.conf's models.<name>
  RAMHint string              // config.conf's ram = …
}

// Existing is what Run needs from one models.<name> already
// configured: where its server and weights are, and its port.
type Existing struct {
  Binary, ModelPath string
  Port              int
  Remote            bool // runs on a cloud machine: paths aren't local
}

// header is one part's parsed metadata and tensor table.
type header struct {
  nums    map[string]int64
  arrays  map[string][]int64
  strs    map[string]string
  tensors []tensorInfo
  end     int64 // where the tensor table ends
}

type tensorInfo struct {
  name   string
  elems  int64
  offset int64
}

// part is one file of a model: its header and its size, which the
// last tensor's size is read from.
type part struct {
  r    io.Reader
  size int64
}

// countReader reads GGUF's little-endian fields, counting bytes and
// keeping the first error, so the parser above reads straight
// through and checks once.
type countReader struct {
  r   *bufio.Reader
  n   int64
  err error
  b   [8]byte
}

type gpuSpec struct {
  bandwidth, tflops float64
}

// alternative is one quantization's row in the comparison.
type alternative struct {
  label  string
  size   int64
  plan   Plan
  picked bool
}

// benchResult is one row of llama-bench -o json.
type benchResult struct {
  NPrompt int     `json:"n_prompt"`
  NGen    int     `json:"n_gen"`
  AvgTS   float64 `json:"avg_ts"`
}

// progressWriter counts bytes and reports at most twice a second.
type progressWriter struct {
  w      io.Writer
  done   int64
  last   time.Time
  report func(int64)
}

// hubRef is a Hugging Face reference taken apart.
type hubRef struct {
  repo, rev, file, quant string
}

type catalogEntry struct {
  Repo, Note string
}
