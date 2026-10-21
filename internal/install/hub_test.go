package install

import (
  "bufio"
  "bytes"
  "context"
  "crypto/sha256"
  "encoding/hex"
  "fmt"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "strings"
  "testing"
  "time"
)

func TestQuantLabel(t *testing.T) {
  for in, want := range map[string]string{
    "Qwen3.5-9B-IQ4_XS.gguf":                        "IQ4_XS",
    "Qwen3-30B-A3B-UD-Q4_K_XL.gguf":                 "UD-Q4_K_XL",
    "Q8_0/Qwen3-32B-Q8_0-00001-of-00002.gguf":       "Q8_0",
    "gpt-oss-20b-MXFP4.gguf":                        "MXFP4",
    "Mellum2.1-12B-A2.5B-Thinking-MXFP4_MOE.gguf":   "MXFP4_MOE",
    "model-bf16.gguf":                               "BF16",
    "plain.gguf":                                    "plain",
  } {
    got := quantLabel(in)
    if got != want {
      t.Errorf("quantLabel(%q) = %q, want %q", in, got, want)
    }
  }
}

func TestGroupQuants(t *testing.T) {
  gs := groupQuants([]hubFile{
    { Path: "Q8_0/m-Q8_0-00002-of-00002.gguf", Size: 30 },
    { Path: "m-Q4_K_M.gguf", Size: 20 },
    { Path: "Q8_0/m-Q8_0-00001-of-00002.gguf", Size: 40 },
  })

  if len(gs) != 2 || gs[0].Label != "Q4_K_M" || gs[1].Label != "Q8_0" {
    t.Fatalf("groups: %+v", gs)
  }

  if gs[1].Size != 70 || !strings.Contains(gs[1].Files[0].Path, "00001") {
    t.Errorf("split group: %+v", gs[1])
  }

  if defaultGroup(gs) != 0 {
    t.Errorf("default group %d", defaultGroup(gs))
  }
}

func TestModelName(t *testing.T) {
  taken := map[string]Existing{ "qwen35-9b": {} }
  for _, c := range []struct{ file, quant, want string }{
    { "/m/Qwen3.5-9B-IQ4_XS.gguf", "IQ4_XS", "qwen35-9b-2" },
    { "/m/Qwen3-32B-Q8_0-00001-of-00002.gguf", "Q8_0", "qwen3-32b" },
    { "/m/Some_Model.gguf", "", "some-model" },
  } {
    got := modelName(c.file, c.quant, taken)
    if got != c.want || !validName(got) {
      t.Errorf("modelName(%q) = %q, want %q", c.file, got, c.want)
    }
  }
}

// hub serves a fake repo: its tree listing and one file, with Range.
func hub(t *testing.T, name string, data []byte) *httptest.Server {
  sum := sha256.Sum256(data)
  srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    switch {
    case strings.HasPrefix(r.URL.Path, "/api/models/org/repo/tree/main"):
      fmt.Fprintf(w,
        `[{"type":"file","path":%q,"size":%d,"lfs":{"oid":%q,"size":%d}},`+
          `{"type":"file","path":"README.md","size":10},`+
          `{"type":"file","path":"mmproj-F16.gguf","size":10},`+
          `{"type":"file","path":"mtp-m.gguf","size":10}]`,
        name, len(data), hex.EncodeToString(sum[:]), len(data),
      )
    case r.URL.Path == "/org/repo/resolve/main/" + name:
      http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
    default:
      http.NotFound(w, r)
    }
  }))

  orig := hubURL
  hubURL = srv.URL
  t.Cleanup(func() { hubURL = orig; srv.Close() })

  return srv
}

func TestResolveAndReadRemote(t *testing.T) {
  data := testMoE()
  srv := hub(t, "m-Q4_K_M.gguf", data)

  ctx := context.Background()
  src, err := resolve(ctx, srv.Client(), "org/repo:q4_k_m")
  if err != nil {
    t.Fatal(err)
  }

  if len(src.Groups) != 1 || src.Pick != 0 || src.Groups[0].Files[0].SHA256 == "" {
    t.Fatalf("source: %+v", src)
  }

  m, size, err := readSource(ctx, srv.Client(), src)
  if err != nil {
    t.Fatal(err)
  }

  if size != int64(len(data)) || m.Arch != "testarch" || m.Experts != 4 * 3200 {
    t.Errorf("remote model: size %d %+v", size, m)
  }

  _, err = resolve(ctx, srv.Client(), "org/repo:Q8_0")
  if err == nil || !strings.Contains(err.Error(), "Q4_K_M") {
    t.Errorf("missing quant: %v", err)
  }
}

func TestDownloadResumes(t *testing.T) {
  data := bytes.Repeat([]byte("0123456789"), 100000)
  srv := hub(t, "m.gguf", data)
  sum := sha256.Sum256(data)
  dest := filepath.Join(t.TempDir(), "m.gguf")

  // A previous run got the first third.
  err := os.WriteFile(dest + ".part", data[:len(data) / 3], 0o644)
  if err != nil {
    t.Fatal(err)
  }

  u := (&source{ Repo: "org/repo", Rev: "main" }).fileURL(hubFile{ Path: "m.gguf" })
  err = download(context.Background(), srv.Client(), u, dest, int64(len(data)), hex.EncodeToString(sum[:]), nil)
  if err != nil {
    t.Fatal(err)
  }

  got, err := os.ReadFile(dest)
  if err != nil || !bytes.Equal(got, data) {
    t.Fatalf("downloaded %d bytes, err %v", len(got), err)
  }

  _, err = os.Stat(dest + ".part")
  if !os.IsNotExist(err) {
    t.Error(".part left behind")
  }

  // A wrong checksum deletes the file instead of keeping it.
  dest2 := dest + "2"
  err = download(context.Background(), srv.Client(), u, dest2, int64(len(data)), strings.Repeat("0", 64), nil)
  if err == nil || !strings.Contains(err.Error(), "SHA-256") {
    t.Errorf("bad checksum: %v", err)
  }

  _, err = os.Stat(dest2 + ".part")
  if !os.IsNotExist(err) {
    t.Error("bad download kept")
  }
}

func TestParseHub(t *testing.T) {
  for in, want := range map[string]hubRef{
    "org/repo":                         { "org/repo", "main", "", "" },
    "org/repo:Q4_K_M":                  { "org/repo", "main", "", "Q4_K_M" },
    "org/repo/sub/m-Q8_0.gguf":         { "org/repo", "main", "sub/m-Q8_0.gguf", "" },
    "https://huggingface.co/org/repo":  { "org/repo", "main", "", "" },
    "https://huggingface.co/org/repo/": { "org/repo", "main", "", "" },
    "http://www.huggingface.co/org/repo?library=gguf#files": { "org/repo", "main", "", "" },
    "https://hf.co/org/repo:IQ4_XS":    { "org/repo", "main", "", "IQ4_XS" },
    "https://huggingface.co/org/repo/tree/main":              { "org/repo", "main", "", "" },
    "https://huggingface.co/org/repo/tree/v2/Q8_0":           { "org/repo", "v2", "", "Q8_0" },
    "https://huggingface.co/org/repo/blob/main/m-Q4_K_M.gguf": { "org/repo", "main", "m-Q4_K_M.gguf", "" },
    "https://huggingface.co/org/repo/resolve/abc123/d/m.gguf?download=true": { "org/repo", "abc123", "d/m.gguf", "" },
  } {
    got, err := parseHub(in)
    if err != nil || got != want {
      t.Errorf("parseHub(%q) = %+v, %v; want %+v", in, got, err, want)
    }
  }

  for _, bad := range []string{
    "repo", "https://huggingface.co/", "https://huggingface.co/datasets/org/data",
    "https://huggingface.co/spaces/org/app",
  } {
    _, err := parseHub(bad)
    if err == nil {
      t.Errorf("parseHub(%q) accepted", bad)
    }
  }
}

func TestCatalog(t *testing.T) {
  e, ok := lookupCatalog("Qwen3.5-9B")
  if !ok || e.Repo != "unsloth/Qwen3.5-9B-GGUF" {
    t.Errorf("lookupCatalog: %+v %v", e, ok)
  }

  for name, e := range catalog {
    _, err := parseHub(e.Repo)
    if err != nil || name != strings.ToLower(name) || strings.Contains(name, ":") {
      t.Errorf("catalog %q: %v", name, err)
    }
  }
}

func TestDirectLink(t *testing.T) {
  data := testMoE()
  srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    if r.URL.Path != "/files/m-Q5_K_M.gguf" {
      http.NotFound(w, r)
      return
    }

    http.ServeContent(w, r, "m.gguf", time.Time{}, bytes.NewReader(data))
  }))
  defer srv.Close()

  ctx := context.Background()
  src, err := resolve(ctx, srv.Client(), srv.URL + "/files/m-Q5_K_M.gguf?x=1")
  if err != nil {
    t.Fatal(err)
  }

  if src.URL == "" || src.Groups[0].Label != "Q5_K_M" || src.Groups[0].Size != int64(len(data)) {
    t.Fatalf("direct source: %+v", src)
  }

  m, _, err := readSource(ctx, srv.Client(), src)
  if err != nil || m.Arch != "testarch" {
    t.Errorf("direct header: %+v %v", m, err)
  }

  _, err = resolve(ctx, srv.Client(), srv.URL + "/files/readme.txt")
  if err == nil {
    t.Error("non-gguf link accepted")
  }
}

func TestConfirm(t *testing.T) {
  src := &source{ Repo: "o/r", Pick: 1, Groups: []quantGroup{
    { Label: "Q4_0", Size: 1 }, { Label: "Q5_K_M", Size: 2 },
  } }

  for _, c := range []struct {
    input string
    v     Verdict
    want  int
  }{
    { "\n", VerdictFine, choiceYes },
    { "y\n", VerdictSlow, choiceYes },
    { "n\n", VerdictFine, choiceNo },
    { "q4_0\n", VerdictFine, 0 },
    { "Q9_9\nQ4_0\n", VerdictFine, 0 }, // a typo asks again
    { "\n", VerdictTooSlow, choiceNo },
    { "y\nQ4_0\n", VerdictWontLoad, 0 }, // yes isn't an answer there
    { "", VerdictFine, choiceYes }, // no terminal: the default
  } {
    in := bufio.NewReader(strings.NewReader(c.input))
    got := confirm(in, false, src, "/m", c.v)
    if got != c.want {
      t.Errorf("confirm(%q, %s) = %d, want %d", c.input, c.v, got, c.want)
    }
  }

  if confirm(nil, true, src, "/m", VerdictWontLoad) != choiceNo {
    t.Error("-yes said yes to a model that won't load")
  }
}

func TestGroupQuantsDuplicateLabels(t *testing.T) {
  gs := groupQuants([]hubFile{
    { Path: "M-Q4_0.gguf", Size: 10 },
    { Path: "M-QAD-Q4_0.gguf", Size: 11 },
    { Path: "M-Q8_0.gguf", Size: 20 },
  })

  if gs[0].Label != "M-Q4_0" || gs[1].Label != "M-QAD-Q4_0" || gs[2].Label != "Q8_0" {
    t.Errorf("labels: %s %s %s", gs[0].Label, gs[1].Label, gs[2].Label)
  }
}
