package install

import (
  "bytes"
  "encoding/binary"
  "math"
  "testing"
)

type testKV struct {
  key string
  val any // uint32, string, []int32, []bool, []string
}

type testTensor struct {
  name string
  dims []uint64
  size int64
}

// buildGGUF writes a GGUF v3 file: kvs, then tensors whose data is
// size bytes each, aligned to 32.
func buildGGUF(kvs []testKV, tensors []testTensor) []byte {
  var b bytes.Buffer
  w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
  str := func(s string) { w(uint64(len(s))); b.WriteString(s) }

  b.WriteString("GGUF")
  w(uint32(3))
  w(uint64(len(tensors)))
  w(uint64(len(kvs)))
  for _, kv := range kvs {
    str(kv.key)
    switch v := kv.val.(type) {
    case uint32:
      w(uint32(ggufU32))
      w(v)
    case float32:
      w(uint32(ggufF32))
      w(math.Float32bits(v))
    case string:
      w(uint32(ggufString))
      str(v)
    case []int32:
      w(uint32(ggufArray))
      w(uint32(ggufI32))
      w(uint64(len(v)))
      for _, x := range v {
        w(x)
      }
    case []bool:
      w(uint32(ggufArray))
      w(uint32(ggufBool))
      w(uint64(len(v)))
      for _, x := range v {
        w(x)
      }
    case []string:
      w(uint32(ggufArray))
      w(uint32(ggufString))
      w(uint64(len(v)))
      for _, x := range v {
        str(x)
      }
    }
  }

  var off int64
  for _, t := range tensors {
    str(t.name)
    w(uint32(len(t.dims)))
    for _, d := range t.dims {
      w(d)
    }

    w(uint32(12))
    w(uint64(off))
    off += (t.size + 31) / 32 * 32
  }

  for b.Len() % 32 != 0 {
    b.WriteByte(0)
  }

  b.Write(make([]byte, off))

  return b.Bytes()
}

// testMoE is a 4-layer MoE: 8 experts, 2 active, layers 1 and 3
// windowed (window 64), with a tokenizer to skip.
func testMoE() []byte {
  kvs := []testKV{
    { "general.architecture", "testarch" },
    { "general.name", "Test MoE" },
    { "general.file_type", uint32(15) },
    { "tokenizer.ggml.tokens", []string{ "a", "b", "c" } },
    { "testarch.block_count", uint32(4) },
    { "testarch.context_length", uint32(65536) },
    { "testarch.embedding_length", uint32(256) },
    { "testarch.attention.head_count", uint32(8) },
    { "testarch.attention.head_count_kv", uint32(2) },
    { "testarch.expert_count", uint32(8) },
    { "testarch.expert_used_count", uint32(2) },
    { "testarch.attention.sliding_window", uint32(64) },
    { "testarch.attention.sliding_window_pattern", []bool{ false, true, false, true } },
  }

  ts := []testTensor{
    { "token_embd.weight", []uint64{ 256, 1000 }, 1000 },
    { "output.weight", []uint64{ 256, 1000 }, 1000 },
  }

  for i := 0; i < 4; i++ {
    p := "blk." + string(rune('0' + i)) + "."
    ts = append(ts,
      testTensor{ p + "attn_q.weight", []uint64{ 256, 256 }, 320 },
      testTensor{ p + "ffn_up_exps.weight", []uint64{ 256, 64, 8 }, 3200 },
    )
  }

  return buildGGUF(kvs, ts)
}

func TestReadModel(t *testing.T) {
  data := testMoE()
  m, err := readModel([]part{ { r: bytes.NewReader(data), size: int64(len(data)) } })
  if err != nil {
    t.Fatal(err)
  }

  if m.Arch != "testarch" || m.Name != "Test MoE" || m.Quant != "Q4_K_M" {
    t.Errorf("identity = %q %q %q", m.Arch, m.Name, m.Quant)
  }

  if m.Layers != 4 || m.Context != 65536 || m.ExpertCount != 8 || m.ExpertUsed != 2 {
    t.Errorf("shape = %+v", m)
  }

  // Sizes come from offset gaps, rounded up to the alignment.
  if m.Embed != 1024 || m.Output != 1024 || m.Experts != 4 * 3200 {
    t.Errorf("embed %d output %d experts %d", m.Embed, m.Output, m.Experts)
  }

  if m.Total != 1024 * 2 + 4 * (320 + 3200) {
    t.Errorf("total = %d", m.Total)
  }

  // 2 kv heads × (32 + 32) dims × q8_0, per attention layer.
  per := 2 * 64 * kvBytesQ8
  if m.AttnLayers != 4 || m.SWALayers != 2 || m.KVFull != 2 * per || m.KVSWA != 2 * per {
    t.Errorf("kv: %d layers, %d swa, full %v swa %v", m.AttnLayers, m.SWALayers, m.KVFull, m.KVSWA)
  }

  // Windowed layers stop growing at window + one batch.
  want := int64(2 * per * 10000 + 2 * per * float64(64 + swaUBatch))
  if m.KVBytes(10000) != want {
    t.Errorf("KVBytes(10000) = %d, want %d", m.KVBytes(10000), want)
  }

  // A token reads 2 of 8 experts and no embedding table.
  if m.ReadPerToken() != m.Total - 1024 - 4 * 3200 * 6 / 8 {
    t.Errorf("ReadPerToken = %d", m.ReadPerToken())
  }
}

func TestReadModelHybrid(t *testing.T) {
  kvs := []testKV{
    { "general.architecture", "hy" },
    { "hy.block_count", uint32(8) },
    { "hy.attention.head_count", uint32(4) },
    { "hy.attention.head_count_kv", uint32(1) },
    { "hy.attention.key_length", uint32(16) },
    { "hy.full_attention_interval", uint32(4) },
  }

  data := buildGGUF(kvs, []testTensor{ { "blk.0.x", []uint64{ 4 }, 64 } })
  m, err := readModel([]part{ { r: bytes.NewReader(data), size: int64(len(data)) } })
  if err != nil {
    t.Fatal(err)
  }

  if m.AttnLayers != 2 || m.KVFull != 2 * 32 * kvBytesQ8 {
    t.Errorf("hybrid: %d attention layers, %v bytes", m.AttnLayers, m.KVFull)
  }

  if m.Tied() != true || m.Kind != "dense, hybrid: 2 of 8 layers attention" {
    t.Errorf("kind %q tied %v", m.Kind, m.Tied())
  }
}

func TestReadModelPerLayerKV(t *testing.T) {
  kvs := []testKV{
    { "general.architecture", "lfm2" },
    { "lfm2.block_count", uint32(4) },
    { "lfm2.attention.head_count", uint32(4) },
    { "lfm2.attention.head_count_kv", []int32{ 0, 2, 0, 2 } },
    { "lfm2.embedding_length", uint32(64) },
  }

  data := buildGGUF(kvs, []testTensor{ { "blk.0.x", []uint64{ 4 }, 64 } })
  m, err := readModel([]part{ { r: bytes.NewReader(data), size: int64(len(data)) } })
  if err != nil {
    t.Fatal(err)
  }

  if m.AttnLayers != 2 || m.KVFull != 2 * 2 * 32 * kvBytesQ8 {
    t.Errorf("per-layer kv: %d layers, %v bytes", m.AttnLayers, m.KVFull)
  }
}

func TestReadModelRejects(t *testing.T) {
  for name, data := range map[string][]byte{
    "not gguf":  []byte("GGML0000000000000000000000"),
    "truncated": testMoE()[:100],
  } {
    _, err := readModel([]part{ { r: bytes.NewReader(data), size: int64(len(data)) } })
    if err == nil {
      t.Errorf("%s: no error", name)
    }
  }

  _, err := readModel([]part{ { r: bytes.NewReader(nil), size: 0 } })
  if err == nil {
    t.Error("empty: no error")
  }
}

func TestReadModelSplit(t *testing.T) {
  a := buildGGUF([]testKV{
    { "general.architecture", "x" },
    { "split.count", uint32(2) },
    { "x.block_count", uint32(1) },
  }, []testTensor{ { "blk.0.a", []uint64{ 4 }, 64 } })
  b := buildGGUF([]testKV{ { "split.count", uint32(2) } }, []testTensor{ { "blk.0.b", []uint64{ 4 }, 96 } })

  m, err := readModel([]part{
    { r: bytes.NewReader(a), size: int64(len(a)) },
    { r: bytes.NewReader(b), size: int64(len(b)) },
  })
  if err != nil {
    t.Fatal(err)
  }

  if m.Total != 64 + 96 || m.Params != 8 {
    t.Errorf("split: total %d params %d", m.Total, m.Params)
  }

  _, err = readModel([]part{ { r: bytes.NewReader(a), size: int64(len(a)) } })
  if err == nil {
    t.Error("one part of two: no error")
  }
}
