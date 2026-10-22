// Where models come from: what -install was given (resolve: a name
// from the table, a Hugging Face repo or link, a direct .gguf link),
// a repo's .gguf
// files grouped by quantization (listRepo), reading a remote header
// a few MB at a time (rangeReader), and the download itself, resumed
// from a .part file and checked against the repo's SHA-256.

package install

import (
  "context"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "os"
  "path"
  "path/filepath"
  "regexp"
  "sort"
  "strings"
  "time"
)

const (
  rangeChunk = 2 << 20
  maxListing = 8 << 20
)

var (
  hubURL      = "https://huggingface.co" // a variable for tests
  splitSuffix = regexp.MustCompile(`-\d{5}-of-(\d{5})\.gguf$`)
  quantTag    = regexp.MustCompile(
    `(?i)(?:^|[-_.])((?:UD-)?(?:I?Q\d(?:_[0-9A-Z]+)*|BF16|F16|F32|MXFP4(?:_MOE)?|TQ\d_0))(?:[-_.]|$)`,
  )
)

// resolve reads -install's argument: a .gguf on disk; a name from
// the built-in table (qwen3.5-9b[:QUANT]); a Hugging Face repo as
// org/repo[:QUANT], org/repo/file.gguf or any huggingface.co / hf.co
// link to it; or a link to a .gguf on any other server.
func resolve(ctx context.Context, c *http.Client, arg string) (*source, error) {
  _, err := os.Stat(arg)
  if err == nil {
    return &source{ Local: arg }, nil
  }

  name, quant, _ := strings.Cut(arg, ":")
  e, ok := lookupCatalog(name)
  if ok {
    arg = e.Repo
    if quant != "" {
      arg += ":" + quant
    }
  }

  if isURL(arg) && !isHubURL(arg) {
    return direct(ctx, c, arg)
  }

  ref, err := parseHub(arg)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in resolve:\n\t%w", err)
  }

  s := &source{ Repo: ref.repo, Rev: ref.rev, Quant: ref.quant, File: ref.file }
  files, err := listRepo(ctx, c, s.Repo, s.Rev)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in resolve:\n\t%w", err)
  }

  s.Groups = groupQuants(files)
  if len(s.Groups) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in resolve:\n\t %s has no .gguf files: armin runs models "+
        "through llama.cpp, so look for a GGUF conversion of it "+
        "(often <org>/<name>-GGUF)",
      s.Repo,
    )
  }

  s.Pick = -1
  switch {
  case ref.file != "":
    s.Pick = findGroup(s.Groups, func(g quantGroup) bool {
      for _, f := range g.Files {
        if f.Path == ref.file {
          return true
        }
      }

      return false
    })
  case ref.quant != "":
    s.Pick = findGroup(s.Groups, func(g quantGroup) bool {
      return strings.EqualFold(g.Label, ref.quant)
    })
    if s.Pick < 0 {
      s.Pick = findGroup(s.Groups, func(g quantGroup) bool {
        return strings.Contains(strings.ToUpper(g.Files[0].Path), strings.ToUpper(ref.quant))
      })
    }
  default:
    s.Pick = defaultGroup(s.Groups)
  }

  if s.Pick < 0 {
    labels := make([]string, len(s.Groups))
    for i, g := range s.Groups {
      labels[i] = g.Label
    }

    return nil, fmt.Errorf(
      "\n\t... in resolve:\n\t %s has no %s%s; it has: %s",
      s.Repo, ref.quant, ref.file, strings.Join(labels, ", "),
    )
  }

  return s, nil
}

// parseHub reads org/repo[:QUANT], org/repo/path/file.gguf, and the
// links the site gives: https://(www.)huggingface.co/org/repo, its
// /tree/<rev>[/folder], /blob/<rev>/file and /resolve/<rev>/file
// forms, hf.co short links, with any ?query or #fragment.
func parseHub(arg string) (hubRef, error) {
  ref := hubRef{ rev: "main" }
  rest := arg
  for _, p := range []string{ "https://", "http://", "www.", "huggingface.co/", "hf.co/" } {
    rest = strings.TrimPrefix(rest, p)
  }

  cut := strings.IndexAny(rest, "?#")
  if cut >= 0 {
    rest = rest[:cut]
  }

  parts := strings.Split(strings.Trim(rest, "/"), "/")
  if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
    return ref, fmt.Errorf(
      "%q: want a .gguf path, a model from armin -install list, "+
        "org/repo[:QUANT] or a Hugging Face link", arg,
    )
  }

  switch parts[0] {
  case "datasets", "spaces", "collections", "models":
    return ref, fmt.Errorf("%q: a link to a model repo, please, not to %s", arg, parts[0])
  }

  repo, quant, _ := strings.Cut(parts[1], ":")
  ref.repo, ref.quant = parts[0] + "/" + repo, quant
  p := parts[2:]
  if len(p) >= 2 && (p[0] == "tree" || p[0] == "blob" || p[0] == "resolve") {
    kind := p[0]
    ref.rev, p = p[1], p[2:]
    if kind == "tree" {
      // A folder of a repo is how split quantizations are kept.
      if len(p) > 0 && ref.quant == "" {
        ref.quant = p[len(p) - 1]
      }

      return ref, nil
    }
  }

  if len(p) > 0 {
    ref.file = strings.Join(p, "/")
  }

  return ref, nil
}

func isURL(s string) bool {
  return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

func isHubURL(s string) bool {
  u, err := url.Parse(s)
  if err != nil {
    return false
  }

  h := strings.TrimPrefix(u.Hostname(), "www.")

  return h == "huggingface.co" || h == "hf.co"
}

// direct is a link to a .gguf on some other server (ModelScope, a
// GitHub release, your own): one file, sized by a one-byte range
// request, with no checksum to check.
func direct(ctx context.Context, c *http.Client, link string) (*source, error) {
  u, err := url.Parse(link)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in direct:\n\t%w", err)
  }

  base := path.Base(u.Path)
  if !strings.HasSuffix(strings.ToLower(base), ".gguf") {
    return nil, fmt.Errorf(
      "\n\t... in direct:\n\t %s: not a .gguf — link the file itself "+
        "(outside Hugging Face, armin can't list a repo)", link,
    )
  }

  size, err := remoteSize(ctx, c, link)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in direct:\n\t%w", err)
  }

  f := hubFile{ Path: base, Size: size }

  return &source{
    URL:    link,
    Groups: []quantGroup{ { Label: quantLabel(base), Files: []hubFile{ f }, Size: size } },
  }, nil
}

// remoteSize asks for one byte and reads the total from
// Content-Range, which also proves the server takes ranges (the
// header is read with them).
func remoteSize(ctx context.Context, c *http.Client, link string) (int64, error) {
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
  if err != nil {
    return 0, err
  }

  req.Header.Set("Range", "bytes=0-0")
  resp, err := c.Do(req)
  if err != nil {
    return 0, err
  }

  defer resp.Body.Close()

  if resp.StatusCode != http.StatusPartialContent {
    return 0, fmt.Errorf("%s: %s (it must serve byte ranges)", link, resp.Status)
  }

  cr := resp.Header.Get("Content-Range")
  i := strings.LastIndexByte(cr, '/')
  if i < 0 {
    return 0, fmt.Errorf("%s: no size in Content-Range %q", link, cr)
  }

  var size int64
  _, err = fmt.Sscan(cr[i + 1:], &size)
  if err != nil || size <= 0 {
    return 0, fmt.Errorf("%s: no size in Content-Range %q", link, cr)
  }

  return size, nil
}

// fileURL is where f downloads from.
func (s *source) fileURL(f hubFile) string {
  if s.URL != "" {
    return s.URL
  }

  return hubURL + "/" + s.Repo + "/resolve/" + s.Rev + "/" + (&url.URL{ Path: f.Path }).EscapedPath()
}

func findGroup(gs []quantGroup, match func(quantGroup) bool) int {
  for i, g := range gs {
    if match(g) {
      return i
    }
  }

  return -1
}

// defaultGroup is where the reading starts when no quantization was
// named: Q4_K_M, the usual balance, or else the middle size.
func defaultGroup(gs []quantGroup) int {
  i := findGroup(gs, func(g quantGroup) bool { return strings.EqualFold(g.Label, "Q4_K_M") })
  if i >= 0 {
    return i
  }

  return len(gs) / 2
}

// listRepo lists a repo's .gguf files, with sizes and checksums.
func listRepo(ctx context.Context, c *http.Client, repo, rev string) ([]hubFile, error) {
  u := hubURL + "/api/models/" + repo + "/tree/" + url.PathEscape(rev) + "?recursive=true"
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in listRepo:\n\t%w", err)
  }

  authorize(req)
  resp, err := c.Do(req)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in listRepo:\n\t%w", err)
  }

  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    hint := ""
    if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
      hint = " — a gated or private repo needs HF_TOKEN in .env"
    }

    return nil, fmt.Errorf(
      "\n\t... in listRepo:\n\t %s: %s%s", repo, resp.Status, hint,
    )
  }

  var entries []struct {
    Type, Path string
    Size       int64
    LFS        *struct {
      OID  string `json:"oid"`
      Size int64  `json:"size"`
    } `json:"lfs"`
  }

  err = json.NewDecoder(io.LimitReader(resp.Body, maxListing)).Decode(&entries)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in listRepo:\n\t %s: %w", repo, err)
  }

  var out []hubFile
  for _, e := range entries {
    base := strings.ToLower(path.Base(e.Path))
    // Vision projectors and multi-token-prediction draft heads go
    // with a model; they aren't one.
    if e.Type != "file" || !strings.HasSuffix(base, ".gguf") ||
      strings.Contains(base, "mmproj") || strings.HasPrefix(base, "mtp-") {
      continue
    }

    f := hubFile{ Path: e.Path, Size: e.Size }
    if e.LFS != nil {
      f.Size, f.SHA256 = e.LFS.Size, e.LFS.OID
    }

    out = append(out, f)
  }

  return out, nil
}

// groupQuants puts a split model's parts together and labels each
// group by its quantization, smallest first.
func groupQuants(files []hubFile) []quantGroup {
  byKey := map[string]*quantGroup{}
  var keys []string
  for _, f := range files {
    key := splitSuffix.ReplaceAllString(f.Path, ".gguf")
    g := byKey[key]
    if g == nil {
      g = &quantGroup{ Label: quantLabel(f.Path) }
      byKey[key] = g
      keys = append(keys, key)
    }

    g.Files = append(g.Files, f)
    g.Size += f.Size
  }

  out := make([]quantGroup, 0, len(keys))
  for _, k := range keys {
    g := byKey[k]
    sort.Slice(g.Files, func(i, j int) bool { return g.Files[i].Path < g.Files[j].Path })
    out = append(out, *g)
  }

  sort.Slice(out, func(i, j int) bool { return out[i].Size < out[j].Size })

  // Two files with the same quantization (Q4_0 and QAD-Q4_0, say)
  // go by their file names, so each can be picked by its label.
  seen := map[string]int{}
  for _, g := range out {
    seen[strings.ToUpper(g.Label)]++
  }

  for i := range out {
    if seen[strings.ToUpper(out[i].Label)] > 1 {
      base := splitSuffix.ReplaceAllString(path.Base(out[i].Files[0].Path), ".gguf")
      out[i].Label = strings.TrimSuffix(base, ".gguf")
    }
  }

  return out
}

// quantLabel is the quantization in a file name ("Q4_K_M"), or the
// name itself when it has none.
func quantLabel(p string) string {
  base := strings.TrimSuffix(splitSuffix.ReplaceAllString(path.Base(p), ".gguf"), ".gguf")
  m := quantTag.FindAllStringSubmatch(base, -1)
  if len(m) == 0 {
    return base
  }

  return strings.ToUpper(m[len(m) - 1][1])
}

// authorize adds HF_TOKEN, for gated and private repos. Go drops it
// when the download redirects to another host (the CDN, whose URL
// is signed instead).
func authorize(req *http.Request) {
  token := os.Getenv("HF_TOKEN")
  if token != "" {
    req.Header.Set("Authorization", "Bearer " + token)
  }
}

func newRangeReader(
  ctx context.Context, c *http.Client, u string, size int64,
) *rangeReader {
  return &rangeReader{ ctx: ctx, client: c, url: u, size: size }
}

// Read serves buffered bytes, fetching the next chunk when they run
// out; a server that ignores Range is an error, not a full download.
func (r *rangeReader) Read(p []byte) (int, error) {
  if r.pos == len(r.buf) {
    if r.off >= r.size {
      return 0, io.EOF
    }

    end := r.off + rangeChunk - 1
    if end >= r.size {
      end = r.size - 1
    }

    req, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
    if err != nil {
      return 0, err
    }

    authorize(req)
    req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", r.off, end))
    resp, err := r.client.Do(req)
    if err != nil {
      return 0, err
    }

    defer resp.Body.Close()

    if resp.StatusCode != http.StatusPartialContent {
      return 0, fmt.Errorf("reading the header: %s (no range support?)", resp.Status)
    }

    r.buf, err = io.ReadAll(io.LimitReader(resp.Body, rangeChunk))
    if err != nil {
      return 0, err
    }

    if len(r.buf) == 0 {
      return 0, io.ErrUnexpectedEOF
    }

    r.pos = 0
    r.off += int64(len(r.buf))
  }

  n := copy(p, r.buf[r.pos:])
  r.pos += n

  return n, nil
}

// Retrying a download: a connection that sends nothing for
// stallTimeout is dropped; each retry waits twice as long as the last,
// from retryFirst up to retryMax; after giveUpAfter with no new byte,
// it stops. Variables so tests can shorten them.
var (
  stallTimeout = 30 * time.Second
  retryFirst   = 5 * time.Second
  retryMax     = 60 * time.Second
  giveUpAfter  = 30 * time.Minute
)

// download fetches u into dest, resuming dest.part if a previous run
// left one, and checks the size and, when known, the SHA-256. A
// dropped or stalled connection is retried from where it stopped
// until the server answers again or giveUpAfter passes without
// progress. progress is called about every half second with the
// bytes done and how many of them this run fetched; retry, before
// each wait, with what failed and how long it waits.
func download(
  ctx context.Context, c *http.Client, u, dest string, size int64,
  sum string, progress func(done, fresh int64),
  retry func(err error, wait time.Duration),
) error {
  partPath := dest + ".part"
  f, err := os.OpenFile(partPath, os.O_RDWR | os.O_CREATE, 0o644)
  if err != nil {
    return fmt.Errorf("\n\t... in download:\n\t%w", err)
  }

  defer f.Close()

  // What's already there goes into the hash first.
  h := sha256.New()
  have, err := io.Copy(h, f)
  if err != nil {
    return fmt.Errorf("\n\t... in download:\n\t%w", err)
  }

  if have > size {
    have = 0
    h.Reset()
    err = f.Truncate(0)
    if err != nil {
      return fmt.Errorf("\n\t... in download:\n\t%w", err)
    }

    _, err = f.Seek(0, io.SeekStart)
    if err != nil {
      return fmt.Errorf("\n\t... in download:\n\t%w", err)
    }
  }

  w := &progressWriter{ w: io.MultiWriter(f, h), done: have, start: have, report: progress }
  wait, progressed := retryFirst, time.Now()
  for w.done < size {
    before := w.done
    err = fetch(ctx, c, u, w)
    if ctx.Err() != nil {
      return errors.New("\n\t... in download:\n\t interrupted — run it again to resume")
    }

    if w.done >= size {
      break
    }

    if err == nil {
      err = errors.New("the server closed the connection early")
    }

    var status *statusError
    if errors.As(err, &status) && !status.retryable() {
      return fmt.Errorf("\n\t... in download:\n\t%w", err)
    }

    if w.done > before {
      wait, progressed = retryFirst, time.Now()
    }

    if time.Since(progressed) > giveUpAfter {
      return fmt.Errorf(
        "\n\t... in download:\n\t no progress for %s (%v) — %s kept; "+
          "run it again to resume",
        giveUpAfter, err, filepath.Base(partPath),
      )
    }

    if retry != nil {
      retry(err, wait)
    }

    select {
    case <-ctx.Done():
      return errors.New("\n\t... in download:\n\t interrupted — run it again to resume")
    case <-time.After(wait):
    }

    wait = min(wait * 2, retryMax)
  }

  info, err := f.Stat()
  if err != nil {
    return fmt.Errorf("\n\t... in download:\n\t%w", err)
  }

  if info.Size() != size {
    return fmt.Errorf(
      "\n\t... in download:\n\t got %d bytes, want %d — run it again to resume",
      info.Size(), size,
    )
  }

  if sum != "" && hex.EncodeToString(h.Sum(nil)) != sum {
    os.Remove(partPath)
    return fmt.Errorf("\n\t... in download:\n\t %s: SHA-256 mismatch, deleted", dest)
  }

  err = f.Close()
  if err != nil {
    return fmt.Errorf("\n\t... in download:\n\t%w", err)
  }

  return os.Rename(partPath, dest)
}

// fetch appends u from byte w.done on through w, dropping the
// connection when nothing arrives for stallTimeout.
func fetch(ctx context.Context, c *http.Client, u string, w *progressWriter) error {
  ctx, cancel := context.WithCancel(ctx)
  defer cancel()

  req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
  if err != nil {
    return err
  }

  authorize(req)
  if w.done > 0 {
    req.Header.Set("Range", fmt.Sprintf("bytes=%d-", w.done))
  }

  resp, err := c.Do(req)
  if err != nil {
    return err
  }

  defer resp.Body.Close()

  switch {
  case w.done > 0 && resp.StatusCode == http.StatusPartialContent:
  case w.done == 0 && resp.StatusCode == http.StatusOK:
  case w.done > 0 && resp.StatusCode == http.StatusOK:
    // The whole file again: appending it would corrupt the part.
    return &statusError{ code: resp.StatusCode, msg: u + ": the server ignores Range, so it can't resume" }
  default:
    return &statusError{ code: resp.StatusCode, msg: u + ": " + resp.Status }
  }

  stall := time.AfterFunc(stallTimeout, cancel)
  defer stall.Stop()

  w.stall = stall
  _, err = io.CopyBuffer(w, resp.Body, make([]byte, 1 << 20))
  w.stall = nil
  if err != nil && ctx.Err() != nil && !stall.Stop() {
    return fmt.Errorf("no data for %s", stallTimeout)
  }

  return err
}

func (e *statusError) Error() string { return e.msg }

// retryable: the server's trouble, or rate limiting, may pass; a
// missing file or a refusal won't.
func (e *statusError) retryable() bool {
  return e.code >= 500 || e.code == http.StatusRequestTimeout ||
    e.code == http.StatusTooManyRequests
}

func (p *progressWriter) Write(b []byte) (int, error) {
  if p.stall != nil {
    p.stall.Reset(stallTimeout)
  }

  n, err := p.w.Write(b)
  p.done += int64(n)
  if p.report != nil && time.Since(p.last) > 500 * time.Millisecond {
    p.last = time.Now()
    p.report(p.done, p.done - p.start)
  }

  return n, err
}
