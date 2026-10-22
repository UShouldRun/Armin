// Package install is armin -install <model>: before downloading a
// model, it reads the model's shape from its .gguf header and the
// computer's from Linux, and runs a decision tree that says whether
// it will load, how fast it will generate and read prompts, and what
// limits each — the GPU's memory bus, system RAM's, PCIe, compute,
// the context memory — with the numbers behind it. Then it
// downloads the model and adds it to config.conf; -uninstall takes
// one out again.
//
//   install.go   Run: resolve → probe → read header → decide →
//                report → download → configure → benchmark
//   gguf.go      the .gguf header: shape, bytes by role, KV per token
//   machine.go   GPUs, RAM, CPU and disk, from sysfs and /proc
//   gputable.go  bandwidth and prefill TFLOPS of known GPUs
//   plan.go      the decision tree and its estimates
//   report.go    printing it
//   hub.go       Hugging Face: listing, header ranges, downloading
//   llama.go     the llama-server build's architectures, llama-bench
//   conf.go      the models.<name> block for config.conf
//   uninstall.go -uninstall: the block and the files, out again
package install

import (
  "bufio"
  "context"
  "errors"
  "fmt"
  "io"
  "net"
  "net/http"
  "os"
  "path"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/ui"
)

// Run is armin -install: size the model up against this machine,
// say so in detail, and on the user's word download and configure it.
func Run(ctx context.Context, req Request) error {
  if req.Model == "list" {
    printCatalog()
    return nil
  }

  client := &http.Client{
    Transport: &http.Transport{
      Proxy:                 http.ProxyFromEnvironment,
      DialContext:           (&net.Dialer{ Timeout: 15 * time.Second }).DialContext,
      TLSHandshakeTimeout:   15 * time.Second,
      ResponseHeaderTimeout: 30 * time.Second,
    },
  }

  in := bufio.NewReader(os.Stdin)
  ask := asker(in, req.Yes)

  src, err := resolve(ctx, client, req.Model)
  if err != nil {
    return fmt.Errorf("\n\t... in Run:\n\t%w", err)
  }

  dir := req.To
  if dir == "" {
    dir = usualDir(req.Models)
  }

  if src.Local != "" {
    dir = filepath.Dir(src.Local)
  }

  hw := probe(dir)
  ramSpeed(&hw, req.RAMHint)
  binary := usualBinary(req.Models)

  m, size, err := readSource(ctx, client, src)
  if err != nil {
    return fmt.Errorf("\n\t... in Run:\n\t%w", err)
  }

  // With no quantization named, the repo's best that runs well.
  if src.Repo != "" && src.Quant == "" && src.File == "" && len(src.Groups) > 1 {
    best := bestGroup(&m, &hw, src)
    if best != src.Pick {
      src.Pick = best
      m, size, err = readSource(ctx, client, src)
      if err != nil {
        return fmt.Errorf("\n\t... in Run:\n\t%w", err)
      }
    }
  }

  // Decide, show, ask; naming another of the repo's quantizations
  // at the question reads that one and goes round again.
  var p Plan
  for first := true; ; first = false {
    p = decide(&m, &hw, need(src, dir))
    known, checked := knowsArch(binary, m.Arch)
    if checked && !known && m.NoRuntime == "" {
      p.add(VerdictWontLoad, CauseRuntime, fmt.Sprintf(
        "%s doesn't know the %q architecture: update llama.cpp first",
        binary, m.Arch,
      ))
    }

    if !first {
      fmt.Println()
    }

    printModel(&m, size)
    if first {
      printMachine(&hw, dir)
    }

    printPlan(&m, &p)
    if len(src.Groups) > 1 {
      printAlternatives(alternatives(&m, &hw, src))
    }

    fmt.Println()
    choice := confirm(in, req.Yes, src, dir, p.Verdict)
    if choice == choiceNo {
      if p.Verdict == VerdictWontLoad {
        return errors.New("not installed: it won't load on this machine")
      }

      return nil
    }

    if choice == choiceYes {
      break
    }

    src.Pick = choice
    m, size, err = readSource(ctx, client, src)
    if err != nil {
      return fmt.Errorf("\n\t... in Run:\n\t%w", err)
    }
  }

  path, err := fetchSource(ctx, client, src, dir)
  if err != nil {
    return err
  }

  name := askLine(in, req.Yes, "name in config.conf", modelName(path, m.Quant, req.Models))
  for !validName(name) {
    name = askLine(in, req.Yes, "no dots, spaces or '=' — name", modelName(path, m.Quant, req.Models))
  }

  origin := filepath.Base(path)
  switch {
  case src.Repo != "":
    origin = src.Repo + ":" + src.Groups[src.Pick].Label
  case src.URL != "":
    origin = src.URL
  }

  conf, err := appendConfig(block(name, binary, path, origin, nextPort(req.Models), &p))
  if err != nil {
    return fmt.Errorf("\n\t... in Run:\n\t%w", err)
  }

  fmt.Printf("added models.%s to %s — use it with armin -model %s or /model %s\n", name, conf, name, name)
  if !req.Yes && ask("run a quick benchmark to check the estimates (up to a minute)?", false) {
    runBench(ctx, binary, path, &p)
  }

  return nil
}

// readSource reads the picked model's header, locally or by ranges.
func readSource(ctx context.Context, c *http.Client, src *source) (Model, int64, error) {
  if src.Local != "" {
    f, err := os.Open(src.Local)
    if err != nil {
      return Model{}, 0, fmt.Errorf("\n\t... in readSource:\n\t%w", err)
    }

    defer f.Close()

    info, err := f.Stat()
    if err != nil {
      return Model{}, 0, fmt.Errorf("\n\t... in readSource:\n\t%w", err)
    }

    m, err := readModel([]part{ { r: f, size: info.Size() } })

    return m, info.Size(), err
  }

  g := src.Groups[src.Pick]
  parts := make([]part, len(g.Files))
  for i, f := range g.Files {
    parts[i] = part{
      r:    newRangeReader(ctx, c, src.fileURL(f), f.Size),
      size: f.Size,
    }
  }

  m, err := readModel(parts)
  if err != nil {
    return Model{}, 0, fmt.Errorf("\n\t... in readSource:\n\t %s: %w", g.Label, err)
  }

  // The file name says what the uploader chose; the header's file
  // type only names the most common tensor type in a mixed quant.
  if quantTag.MatchString(path.Base(g.Files[0].Path)) || m.Quant == "" {
    m.Quant = g.Label
  }

  if looksLikeHash(m.Name) {
    m.Name = strings.TrimSuffix(path.Base(firstNonEmpty(src.Repo, g.Files[0].Path)), ".gguf")
  }

  return m, g.Size, nil
}

// looksLikeHash: some GGUFs carry a commit hash as their name.
func looksLikeHash(s string) bool {
  if len(s) < 32 {
    return false
  }

  for i := 0; i < len(s); i++ {
    c := s[i] | 0x20
    if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
      return false
    }
  }

  return true
}

// need is what's left to download into dir for the picked group.
func need(src *source, dir string) int64 {
  if src.Local != "" {
    return 0
  }

  var n int64
  for _, f := range src.Groups[src.Pick].Files {
    info, err := os.Stat(filepath.Join(dir, filepath.Base(f.Path)))
    if err == nil && info.Size() == f.Size {
      continue
    }

    n += f.Size
  }

  return n
}

// alternatives runs the tree for every quantization in the repo,
// each estimated as the read one scaled to its size.
func alternatives(m *Model, hw *Machine, src *source) []alternative {
  read := float64(src.Groups[src.Pick].Size)
  out := make([]alternative, len(src.Groups))
  for i, g := range src.Groups {
    sm := m.scaled(float64(g.Size) / read)
    sm.Quant = g.Label
    out[i] = alternative{
      label:  g.Label,
      size:   g.Size,
      plan:   decide(&sm, hw, g.Size),
      picked: i == src.Pick,
    }
  }

  return out
}

// bestGroup is, in order: the largest quantization that fits all in
// VRAM and runs fine; the largest that runs fine; the largest that
// merely runs slow; else the one already picked. Below 3 bits
// quality drops sharply and above 8 it stops rising (Q8_0 is close
// to lossless at half of F16's bytes), so those never win.
func bestGroup(m *Model, hw *Machine, src *source) int {
  alts := alternatives(m, hw, src)
  tiers := []func(Plan) bool{
    func(p Plan) bool { return p.Verdict == VerdictFine && p.Place == PlaceGPU },
    func(p Plan) bool { return p.Verdict == VerdictFine },
    func(p Plan) bool { return p.Verdict == VerdictSlow },
  }

  for _, ok := range tiers {
    best := -1
    for i, a := range alts {
      if ok(a.plan) && !lowBit(a.label) && !wideBit(a.label) {
        best = i
      }
    }

    if best >= 0 {
      return best
    }
  }

  return src.Pick
}

func lowBit(label string) bool {
  l := strings.TrimPrefix(strings.ToUpper(label), "UD-")
  return strings.HasPrefix(l, "IQ1") || strings.HasPrefix(l, "IQ2") ||
    strings.HasPrefix(l, "Q2") || strings.HasPrefix(l, "TQ")
}

func wideBit(label string) bool {
  switch strings.ToUpper(label) {
  case "F16", "BF16", "F32":
    return true
  }

  return false
}

const (
  choiceYes = -1
  choiceNo  = -2
)

// confirm asks whether to go ahead with the picked model: yes, no,
// or, for a repo, the label of another quantization to look at
// (returned as its index). A model that won't load can't be said
// yes to, only swapped.
func confirm(in *bufio.Reader, yes bool, src *source, dir string, v Verdict) int {
  if yes {
    if v == VerdictWontLoad {
      return choiceNo
    }

    return choiceYes
  }

  others := len(src.Groups) > 1
  var q string
  switch {
  case v == VerdictWontLoad && !others:
    return choiceNo
  case v == VerdictWontLoad:
    q = "it won't load — another quantization from the list? [name, or Enter to stop] "
  case src.Local != "":
    q = "add it to config.conf?"
  default:
    g := src.Groups[src.Pick]
    q = fmt.Sprintf("download %s (%s) to %s?", g.Label, gb(g.Size), dir)
  }

  if v == VerdictTooSlow {
    q = "it would be too slow to use — " + q
  }

  def := v != VerdictTooSlow
  if v != VerdictWontLoad {
    hint := " [y/N"
    if def {
      hint = " [Y/n"
    }

    if others {
      hint += ", or a quantization from the list"
    }

    q += hint + "] "
  }

  for {
    fmt.Print(q)
    line, err := in.ReadString('\n')
    answer := strings.TrimSpace(line)
    switch strings.ToLower(answer) {
    case "":
      if def && v != VerdictWontLoad {
        return choiceYes
      }

      return choiceNo
    case "y", "yes":
      if v != VerdictWontLoad {
        return choiceYes
      }
    case "n", "no":
      return choiceNo
    }

    i := findGroup(src.Groups, func(g quantGroup) bool { return strings.EqualFold(g.Label, answer) })
    if i >= 0 && others {
      return i
    }

    if err != nil {
      return choiceNo
    }

    fmt.Printf("%q isn't in the list above\n", answer)
  }
}

// fetchSource downloads the picked group into dir and returns the
// path config.conf should name (a split model's first part).
func fetchSource(ctx context.Context, c *http.Client, src *source, dir string) (string, error) {
  if src.Local != "" {
    abs, err := filepath.Abs(src.Local)
    if err != nil {
      return "", fmt.Errorf("\n\t... in fetchSource:\n\t%w", err)
    }

    return abs, nil
  }

  g := src.Groups[src.Pick]
  err := os.MkdirAll(dir, 0o755)
  if err != nil {
    return "", fmt.Errorf("\n\t... in fetchSource:\n\t%w", err)
  }

  var first string
  for i, f := range g.Files {
    dest := filepath.Join(dir, filepath.Base(f.Path))
    if i == 0 {
      first = dest
    }

    info, err := os.Stat(dest)
    if err == nil && info.Size() == f.Size {
      fmt.Println(ui.Dim("already there: " + dest))
      continue
    }

    start := time.Now()
    label := filepath.Base(f.Path)
    err = download(
      ctx, c, src.fileURL(f), dest, f.Size, f.SHA256,
      func(done, fresh int64) { progressLine(label, done, fresh, f.Size, start) },
      func(err error, wait time.Duration) {
        ui.ClearLine()
        fmt.Printf("%s  %s — retrying in %s (Ctrl-C stops; it resumes later)", label, shortErr(err), wait)
      },
    )
    ui.ClearLine()
    if err != nil {
      return "", fmt.Errorf("\n\t... in fetchSource:\n\t%w", err)
    }

    fmt.Printf("downloaded %s (%s)\n", dest, gb(f.Size))
  }

  return first, nil
}

// progressLine is one download's line: the rate counts only what
// this run fetched, not what a previous one left.
func progressLine(label string, done, fresh, size int64, start time.Time) {
  secs := time.Since(start).Seconds()
  rate := float64(fresh) / secs
  eta := ""
  if rate > 0 {
    eta = duration(float64(size - done) / rate)
  }

  ui.ClearLine()
  fmt.Printf(
    "%s  %s / %s  %.0f MB/s  %s left",
    label, gb(done), gb(size), rate / 1e6, eta,
  )
}

// shortErr is an error's last line: the cause, without the trace.
func shortErr(err error) string {
  msg := strings.TrimSpace(err.Error())
  i := strings.LastIndex(msg, "\n")
  if i >= 0 {
    msg = strings.TrimSpace(msg[i + 1:])
  }

  if len(msg) > 60 {
    msg = msg[:57] + "…"
  }

  return "connection lost: " + msg
}

// ramSpeed fills the RAM's type and speed when SMBIOS couldn't (it
// needs root): from config.conf's ram = …, else assumed from the
// CPU's age, with a note saying how to set it.
func ramSpeed(hw *Machine, hint string) {
  r := &hw.RAM
  if r.Bandwidth > 0 {
    return
  }

  typ, speed, ch, ok := parseRAM(hint)
  r.Source = "from config.conf's ram"
  if !ok {
    guess := guessRAM(hw.CPU)
    typ, speed, ch, _ = parseRAM(guess)
    r.Source = "assumed"
    hw.Notes = append(hw.Notes, fmt.Sprintf(
      "RAM type and speed need root to read; assumed %s dual-channel. "+
        "For exact numbers re-run ./install.sh, or set ram = "+
        "<type>-<speed> (ddr5-5600, lpddr5x-7500, ddr4-3200 1ch, …) "+
        "in config.conf", strings.ToUpper(guess),
    ))
  }

  r.Type, r.Speed, r.Channels = typ, speed, ch
  r.Bandwidth = ramBandwidth(speed, ch)
}

func runBench(ctx context.Context, binary, path string, p *Plan) {
  fmt.Println(ui.Dim("running llama-bench…"))
  pre, gen, err := bench(ctx, binary, path, p)
  if err != nil {
    ui.Error("benchmark: %s", err)
    return
  }

  fmt.Printf(
    "measured: generation %.0f tok/s (estimated %.0f), prefill %.0f tok/s (estimated %.0f)\n",
    gen, p.GenEmpty, pre, p.Prefill,
  )
}

// asker is a yes/no question on in, answering def without a
// terminal, and yes to everything under -yes.
func asker(in *bufio.Reader, yes bool) func(string, bool) bool {
  return func(q string, def bool) bool {
    if yes {
      return true
    }

    hint := " [y/N] "
    if def {
      hint = " [Y/n] "
    }

    fmt.Print(q + hint)
    line, err := in.ReadString('\n')
    if err != nil && !errors.Is(err, io.EOF) {
      return def
    }

    switch strings.ToLower(strings.TrimSpace(line)) {
    case "":
      return def
    case "y", "yes":
      return true
    }

    return false
  }
}

// askLine asks for a line, def on Enter or under -yes.
func askLine(in *bufio.Reader, yes bool, q, def string) string {
  if yes {
    return def
  }

  fmt.Printf("%s [%s]: ", q, def)
  line, _ := in.ReadString('\n')
  line = strings.TrimSpace(line)
  if line == "" {
    return def
  }

  return line
}
