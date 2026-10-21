// Reading a .gguf's header: readModel parses the metadata and the
// tensor table of every part (a split model has several), never the
// weights, and sums what the decision tree needs: bytes by role,
// parameters, and the KV cache a token costs.

package install

import (
  "bufio"
  "encoding/binary"
  "errors"
  "fmt"
  "io"
  "math"
  "sort"
  "strings"
)

// GGUF value types.
const (
  ggufU8 = iota
  ggufI8
  ggufU16
  ggufI16
  ggufU32
  ggufI32
  ggufF32
  ggufBool
  ggufString
  ggufArray
  ggufU64
  ggufI64
  ggufF64
)

const (
  maxTensors  = 1 << 20
  maxNameLen  = 1 << 16
  keptArray   = 4096 // arrays longer than this are skipped, not kept
  kvBytesQ8   = 34.0 / 32.0 // q8_0: 32 values in 34 bytes
  swaUBatch   = 512 // llama.cpp keeps window + one batch per SWA layer
)

// fileTypes are general.file_type's names (llama_ftype).
var fileTypes = map[int64]string{
  0: "F32", 1: "F16", 2: "Q4_0", 3: "Q4_1", 7: "Q8_0", 8: "Q5_0",
  9: "Q5_1", 10: "Q2_K", 11: "Q3_K_S", 12: "Q3_K_M", 13: "Q3_K_L",
  14: "Q4_K_S", 15: "Q4_K_M", 16: "Q5_K_S", 17: "Q5_K_M", 18: "Q6_K",
  19: "IQ2_XXS", 20: "IQ2_XS", 21: "Q2_K_S", 22: "IQ3_XS",
  23: "IQ3_XXS", 24: "IQ1_S", 25: "IQ4_NL", 26: "IQ3_S", 27: "IQ3_M",
  28: "IQ2_S", 29: "IQ2_M", 30: "IQ4_XS", 31: "IQ1_M", 32: "BF16",
  36: "TQ1_0", 37: "TQ2_0", 38: "MXFP4_MOE",
}

// recurrent architectures keep a fixed-size state, no KV cache.
var recurrent = map[string]bool{
  "mamba": true, "mamba2": true, "rwkv6": true, "rwkv6qwen2": true,
  "rwkv7": true, "arwkv7": true,
}

// swaPatterns are architectures whose sliding-window layout llama.cpp
// hard-codes instead of reading it: every n-th layer is full.
var swaPatterns = map[string]int{
  "gemma2": 2, "gemma3": 6, "gemma3n": 5, "gpt-oss": 2, "cohere2": 4,
}

// noRuntime are architectures llama-server can't serve.
var noRuntime = map[string]string{
  "dream": "a diffusion model: llama.cpp runs it only through " +
    "llama-diffusion-cli, not llama-server, so armin can't serve it",
  "llada": "a diffusion model: llama.cpp runs it only through " +
    "llama-diffusion-cli, not llama-server, so armin can't serve it",
  "llada-moe": "a diffusion model: llama.cpp runs it only through " +
    "llama-diffusion-cli, not llama-server, so armin can't serve it",
}

// readModel reads every part's header and adds them up into a Model.
func readModel(parts []part) (Model, error) {
  if len(parts) == 0 {
    return Model{}, errors.New("\n\t... in readModel:\n\t no files")
  }

  var first header
  var m Model
  for i, p := range parts {
    h, err := readHeader(p.r)
    if err != nil {
      return Model{}, fmt.Errorf(
        "\n\t... in readModel:\n\t part %d: %w", i + 1, err,
      )
    }

    if i == 0 {
      first = h
    }

    err = m.addTensors(h, p.size)
    if err != nil {
      return Model{}, fmt.Errorf(
        "\n\t... in readModel:\n\t part %d: %w", i + 1, err,
      )
    }
  }

  want := first.nums["split.count"]
  if want > 1 && int64(len(parts)) != want {
    return Model{}, fmt.Errorf(
      "\n\t... in readModel:\n\t the model is split in %d files, got %d",
      want, len(parts),
    )
  }

  m.describe(first)

  return m, nil
}

// addTensors sizes h's tensors from the gaps between their offsets
// (the last one runs to the end of the file), so a quantization type
// armin has never heard of still sizes right, and files each under
// its role.
func (m *Model) addTensors(h header, fileSize int64) error {
  align := h.nums["general.alignment"]
  if align <= 0 {
    align = 32
  }

  dataStart := (h.end + align - 1) / align * align
  if dataStart > fileSize {
    return fmt.Errorf(
      "\n\t... in addTensors:\n\t header ends past the file (%d > %d)",
      dataStart, fileSize,
    )
  }

  ts := h.tensors
  sort.Slice(ts, func(i, j int) bool { return ts[i].offset < ts[j].offset })
  for i, t := range ts {
    end := fileSize - dataStart
    if i + 1 < len(ts) {
      end = ts[i + 1].offset
    }

    size := end - t.offset
    if size < 0 {
      return fmt.Errorf("\n\t... in addTensors:\n\t tensor %s overlaps", t.name)
    }

    m.Total += size
    m.Params += t.elems
    switch {
    case t.name == "token_embd.weight":
      m.Embed += size
      m.EmbedParams += t.elems
      m.NonLayer += size
    case t.name == "output.weight":
      m.Output += size
      m.NonLayer += size
    case strings.Contains(t.name, "_exps."):
      m.Experts += size
      m.ExpertParams += t.elems
    case !strings.HasPrefix(t.name, "blk."):
      m.NonLayer += size
    }
  }

  return nil
}

// describe fills the shape from the first part's metadata: layers,
// experts, which layers keep a KV cache and how much per token.
func (m *Model) describe(h header) {
  arch := h.strs["general.architecture"]
  num := func(k string) int { return int(h.nums[arch + "." + k]) }

  m.Arch, m.Name = arch, h.strs["general.name"]
  m.Quant = fileTypes[h.nums["general.file_type"]]
  m.Layers, m.Context = num("block_count"), num("context_length")
  m.ExpertCount, m.ExpertUsed = num("expert_count"), num("expert_used_count")
  m.NoRuntime = noRuntime[arch]

  heads := perLayer(h, arch + ".attention.head_count", m.Layers, 0)
  kvHeads := perLayer(h, arch + ".attention.head_count_kv", m.Layers, -1)
  for i := range kvHeads {
    if kvHeads[i] < 0 {
      kvHeads[i] = heads[i]
    }
  }

  m.Heads = int(maxOf(heads))
  keyLen, valLen := num("attention.key_length"), num("attention.value_length")
  if keyLen == 0 && m.Heads > 0 {
    keyLen = num("embedding_length") / m.Heads
  }

  if valLen == 0 {
    valLen = keyLen
  }

  m.HeadDim = keyLen
  full := num("full_attention_interval")
  m.Window = num("attention.sliding_window")
  swa := swaLayers(h, arch, m.Layers, m.Window)
  for il := 0; il < m.Layers; il++ {
    if recurrent[arch] || full > 0 && (il + 1) % full != 0 {
      continue
    }

    if kvHeads[il] <= 0 {
      continue
    }

    perToken := float64(kvHeads[il] * int64(keyLen + valLen)) * kvBytesQ8
    m.AttnLayers++
    if swa[il] {
      m.SWALayers++
      m.KVSWA += perToken
    } else {
      m.KVFull += perToken
    }
  }

  m.Kind = "dense"
  if m.ExpertCount > 0 && m.Experts > 0 {
    m.Kind = fmt.Sprintf("MoE, %d experts, %d active", m.ExpertCount, m.ExpertUsed)
  }

  if m.AttnLayers > 0 && m.AttnLayers < m.Layers {
    m.Kind += fmt.Sprintf(
      ", hybrid: %d of %d layers attention", m.AttnLayers, m.Layers,
    )
  }

  if m.AttnLayers == 0 && m.Layers > 0 {
    m.Kind += ", recurrent: no KV cache"
  }
}

// perLayer is a per-layer value that may be stored as one number or
// as an array; def where neither is.
func perLayer(h header, key string, layers int, def int64) []int64 {
  out := make([]int64, layers)
  arr, isArr := h.arrays[key]
  n, isNum := h.nums[key]
  for i := range out {
    switch {
    case isArr && i < len(arr):
      out[i] = arr[i]
    case isNum:
      out[i] = n
    default:
      out[i] = def
    }
  }

  return out
}

// swaLayers marks the sliding-window layers: from the pattern array
// (true = windowed), from a pattern number n (all but every n-th),
// or from what llama.cpp hard-codes for arch.
func swaLayers(h header, arch string, layers, window int) []bool {
  out := make([]bool, layers)
  if window <= 0 {
    return out
  }

  key := arch + ".attention.sliding_window_pattern"
  arr, isArr := h.arrays[key]
  n := int(h.nums[key])
  if n == 0 {
    n = swaPatterns[arch]
  }

  for il := range out {
    switch {
    case isArr && il < len(arr):
      out[il] = arr[il] != 0
    case n > 0:
      out[il] = il % n < n - 1
    }
  }

  return out
}

func maxOf(xs []int64) int64 {
  var m int64
  for _, x := range xs {
    if x > m {
      m = x
    }
  }

  return m
}

// KVBytes is the KV cache for ctx cached tokens: full layers keep
// all of them, windowed ones the window plus a batch.
func (m *Model) KVBytes(ctx int) int64 {
  swa := ctx
  if m.Window > 0 && m.Window + swaUBatch < ctx {
    swa = m.Window + swaUBatch
  }

  return int64(m.KVFull * float64(ctx) + m.KVSWA * float64(swa))
}

// Tied: no output.weight, so token_embd doubles as the output layer
// and is read every token.
func (m *Model) Tied() bool { return m.Output == 0 }

// activeShare is the fraction of the experts a token runs through.
func (m *Model) activeShare() float64 {
  if m.ExpertCount <= 0 || m.ExpertUsed <= 0 {
    return 1
  }

  return float64(m.ExpertUsed) / float64(m.ExpertCount)
}

// ReadPerToken is the weight bytes one generated token reads: all
// but the embedding table (a row lookup) and the idle experts.
func (m *Model) ReadPerToken() int64 {
  b := m.Total - int64(float64(m.Experts) * (1 - m.activeShare()))
  if !m.Tied() {
    b -= m.Embed
  }

  return b
}

// ActiveParams is the weights one token multiplies through.
func (m *Model) ActiveParams() int64 {
  p := m.Params - int64(float64(m.ExpertParams) * (1 - m.activeShare()))
  if !m.Tied() {
    p -= m.EmbedParams
  }

  return p
}

// GPUWeights is what fully offloading puts in VRAM: llama.cpp keeps
// token_embd on the host unless it's also the output layer.
func (m *Model) GPUWeights() int64 {
  if m.Tied() {
    return m.Total
  }

  return m.Total - m.Embed
}

// scaled is m as another quantization of the same model would be:
// every byte count times f, the shape unchanged.
func (m Model) scaled(f float64) Model {
  s := func(b int64) int64 { return int64(float64(b) * f) }
  m.Total, m.Experts, m.Embed = s(m.Total), s(m.Experts), s(m.Embed)
  m.Output, m.NonLayer = s(m.Output), s(m.NonLayer)

  return m
}

// readHeader parses a GGUF header up to the end of its tensor table.
// The tokenizer's arrays (hundreds of thousands of strings) are
// skipped, not kept.
func readHeader(r io.Reader) (header, error) {
  cr := &countReader{ r: bufio.NewReaderSize(r, 64 * 1024) }
  h := header{
    nums:   map[string]int64{},
    arrays: map[string][]int64{},
    strs:   map[string]string{},
  }

  var magic [4]byte
  _, err := io.ReadFull(cr, magic[:])
  if err != nil {
    return h, fmt.Errorf("\n\t... in readHeader:\n\t%w", err)
  }

  if string(magic[:]) != "GGUF" {
    return h, errors.New("\n\t... in readHeader:\n\t not a GGUF file")
  }

  version := cr.u32()
  if version < 2 {
    return h, fmt.Errorf("\n\t... in readHeader:\n\t GGUF v%d is too old", version)
  }

  nTensors, nKV := cr.u64(), cr.u64()
  if nTensors > maxTensors {
    return h, fmt.Errorf("\n\t... in readHeader:\n\t %d tensors", nTensors)
  }

  for i := uint64(0); i < nKV && cr.err == nil; i++ {
    key := cr.str(maxNameLen)
    cr.value(&h, key, cr.u32())
  }

  h.tensors = make([]tensorInfo, 0, nTensors)
  for i := uint64(0); i < nTensors && cr.err == nil; i++ {
    t := tensorInfo{ name: cr.str(maxNameLen), elems: 1 }
    nDims := cr.u32()
    if nDims > 8 {
      return h, fmt.Errorf("\n\t... in readHeader:\n\t %s has %d dims", t.name, nDims)
    }

    for d := uint32(0); d < nDims; d++ {
      t.elems *= int64(cr.u64())
    }

    cr.u32() // the type: sizes come from the offsets instead
    t.offset = int64(cr.u64())
    h.tensors = append(h.tensors, t)
  }

  if cr.err != nil {
    return h, fmt.Errorf("\n\t... in readHeader:\n\t%w", cr.err)
  }

  h.end = cr.n

  return h, nil
}

func (c *countReader) Read(p []byte) (int, error) {
  n, err := c.r.Read(p)
  c.n += int64(n)

  return n, err
}

func (c *countReader) fill(n int) []byte {
  if c.err != nil {
    return c.b[:n]
  }

  _, c.err = io.ReadFull(c, c.b[:n])

  return c.b[:n]
}

func (c *countReader) u32() uint32 { return binary.LittleEndian.Uint32(c.fill(4)) }
func (c *countReader) u64() uint64 { return binary.LittleEndian.Uint64(c.fill(8)) }

// skip discards n bytes without keeping them.
func (c *countReader) skip(n uint64) {
  if c.err != nil {
    return
  }

  var got int64
  got, c.err = io.CopyN(io.Discard, c, int64(n))
  if c.err == nil && uint64(got) != n {
    c.err = io.ErrUnexpectedEOF
  }
}

// str reads a string, or skips it when longer than limit.
func (c *countReader) str(limit uint64) string {
  n := c.u64()
  if c.err != nil {
    return ""
  }

  if n > limit {
    c.skip(n)
    return ""
  }

  b := make([]byte, n)
  _, c.err = io.ReadFull(c, b)

  return string(b)
}

// scalar reads one value of type t as an int64 (floats truncated;
// none of the keys armin reads are fractional).
func (c *countReader) scalar(t uint32) (int64, bool) {
  switch t {
  case ggufU8, ggufI8, ggufBool:
    b := c.fill(1)[0]
    if t == ggufI8 {
      return int64(int8(b)), true
    }

    return int64(b), true
  case ggufU16, ggufI16:
    v := binary.LittleEndian.Uint16(c.fill(2))
    if t == ggufI16 {
      return int64(int16(v)), true
    }

    return int64(v), true
  case ggufU32:
    return int64(c.u32()), true
  case ggufI32:
    return int64(int32(c.u32())), true
  case ggufF32:
    return int64(math.Float32frombits(c.u32())), true
  case ggufU64, ggufI64:
    return int64(c.u64()), true
  case ggufF64:
    return int64(math.Float64frombits(c.u64())), true
  }

  return 0, false
}

// width is a scalar type's size in bytes, 0 for string and array.
func width(t uint32) uint64 {
  switch t {
  case ggufU8, ggufI8, ggufBool:
    return 1
  case ggufU16, ggufI16:
    return 2
  case ggufU32, ggufI32, ggufF32:
    return 4
  case ggufU64, ggufI64, ggufF64:
    return 8
  }

  return 0
}

// value reads key's value of type t into h: numbers, short numeric
// arrays and short strings are kept; the rest is skipped.
func (c *countReader) value(h *header, key string, t uint32) {
  switch t {
  case ggufString:
    s := c.str(1024)
    if !strings.HasPrefix(key, "tokenizer.") {
      h.strs[key] = s
    }
  case ggufArray:
    et, n := c.u32(), c.u64()
    if et == ggufString {
      for i := uint64(0); i < n && c.err == nil; i++ {
        c.skip(c.u64())
      }

      return
    }

    w := width(et)
    if w == 0 {
      c.err = fmt.Errorf("%s: array of type %d", key, et)
      return
    }

    if n > keptArray {
      c.skip(n * w)
      return
    }

    arr := make([]int64, n)
    for i := range arr {
      arr[i], _ = c.scalar(et)
    }

    h.arrays[key] = arr
  default:
    v, ok := c.scalar(t)
    if !ok {
      c.err = fmt.Errorf("%s: unknown type %d", key, t)
      return
    }

    h.nums[key] = v
  }
}
