package library

import (
  "bufio"
  "context"
  "encoding/binary"
  "encoding/gob"
  "fmt"
  "hash/fnv"
  "io"
  "os"
  "runtime"
  "sort"
  "strconv"
  "strings"
  "sync"
)

const (
  indexVersion = 1
  sparseEvery  = 64
  chunkSize    = 24 // bytes per record in chunks.bin
)

// Build (re)indexes the library: documents new or changed since
// their cached extraction are read again (in parallel), then the
// whole index is rewritten from the cache and swapped in. Searches
// keep using the old index meanwhile.
func (l *Library) Build(ctx context.Context, progress Progress) error {
  l.mu.Lock()
  if l.building != "" {
    l.mu.Unlock()
    return fmt.Errorf("\n\t... in Build:\n\t already indexing (%s)", l.building)
  }

  l.building = "scanning"
  l.mu.Unlock()

  defer func() {
    l.mu.Lock()
    l.building = ""
    l.mu.Unlock()
  }()

  report := func(done, total int, what string) {
    l.mu.Lock()
    l.building = fmt.Sprintf("%d/%d %s", done, total, what)
    l.mu.Unlock()
    if progress != nil {
      progress(done, total, what)
    }
  }

  docs, err := l.scan()
  if err != nil {
    return fmt.Errorf("\n\t... in Build:\n\t%w", err)
  }

  err = l.extractAll(ctx, docs, report)
  if err != nil {
    return fmt.Errorf("\n\t... in Build:\n\t%w", err)
  }

  tmp := l.dir("index.tmp")
  os.RemoveAll(tmp)
  err = os.MkdirAll(tmp, 0o755)
  if err != nil {
    return fmt.Errorf("\n\t... in Build:\n\t%w", err)
  }

  docs, err = l.write(ctx, tmp, docs, report)
  if err != nil {
    os.RemoveAll(tmp)
    return fmt.Errorf("\n\t... in Build:\n\t%w", err)
  }

  // Open files of the old index stay valid after the rename.
  final := l.dir("index")
  old := l.dir("index.old")
  os.RemoveAll(old)
  os.Rename(final, old)
  err = os.Rename(tmp, final)
  if err != nil {
    return fmt.Errorf("\n\t... in Build:\n\t%w", err)
  }

  os.RemoveAll(old)
  l.pruneCache(docs)

  idx, err := l.load()
  if err != nil {
    return fmt.Errorf("\n\t... in Build:\n\t%w", err)
  }

  l.mu.Lock()
  prev := l.idx
  l.idx = idx
  l.mu.Unlock()
  prev.close()

  return nil
}

// cachePath is where a document's extraction is kept; the name
// covers its size and mtime, so a changed file misses the cache.
func (l *Library) cachePath(d Doc) string {
  h := fnv.New64a()
  fmt.Fprintf(h, "%s\x00%d\x00%d", d.Path, d.Size, d.Mtime)

  return l.dir("cache", strconv.FormatUint(h.Sum64(), 16) + ".gob")
}

// readCache returns a document's cached pieces.
func (l *Library) readCache(d Doc) ([]piece, bool) {
  f, err := os.Open(l.cachePath(d))
  if err != nil {
    return nil, false
  }

  defer f.Close()

  var c cached
  err = gob.NewDecoder(bufio.NewReader(f)).Decode(&c)
  if err != nil {
    return nil, false
  }

  return c.Pieces, true
}

func (l *Library) fresh(d Doc) bool {
  _, err := os.Stat(l.cachePath(d))
  return err == nil
}

func (l *Library) writeCache(d Doc, pieces []piece) error {
  path := l.cachePath(d)
  f, err := os.Create(path + ".tmp")
  if err != nil {
    return fmt.Errorf("\n\t... in writeCache:\n\t%w", err)
  }

  w := bufio.NewWriter(f)
  c := cached{ Size: d.Size, Mtime: d.Mtime, Pieces: pieces }
  err = gob.NewEncoder(w).Encode(c)
  if err == nil {
    err = w.Flush()
  }

  f.Close()
  if err != nil {
    os.Remove(path + ".tmp")
    return fmt.Errorf("\n\t... in writeCache:\n\t%w", err)
  }

  return os.Rename(path + ".tmp", path)
}

// extractAll reads every document whose cache is missing or stale,
// on half the CPUs (pdftotext is the slow part).
func (l *Library) extractAll(
  ctx context.Context, docs []Doc, report Progress,
) error {
  var todo []Doc
  for _, d := range docs {
    if !l.fresh(d) {
      todo = append(todo, d)
    }
  }

  if len(todo) == 0 {
    return nil
  }

  workers := max(2, runtime.NumCPU() / 2)
  jobs := make(chan Doc)
  var (
    wg   sync.WaitGroup
    mu   sync.Mutex
    done int
  )

  for range workers {
    wg.Add(1)
    go func() {
      defer wg.Done()
      for d := range jobs {
        pieces, err := extract(ctx, l.Abs(d.Path), d.Kind)
        // a failed write only means reading it again next time
        if err == nil {
          l.writeCache(d, pieces)
        }

        mu.Lock()
        done++
        report(done, len(todo), "reading " + d.Path)
        mu.Unlock()
      }
    }()
  }

  for _, d := range todo {
    if ctx.Err() != nil {
      break
    }

    jobs <- d
  }

  close(jobs)
  wg.Wait()

  return ctx.Err()
}

// pruneCache drops cache files of documents no longer present.
func (l *Library) pruneCache(docs []Doc) {
  keep := make(map[string]bool, len(docs))
  for _, d := range docs {
    keep[l.cachePath(d)] = true
  }

  entries, _ := os.ReadDir(l.dir("cache"))
  for _, e := range entries {
    p := l.dir("cache", e.Name())
    if !keep[p] {
      os.Remove(p)
    }
  }
}

// write builds the index files in dir from the cache. Documents
// that couldn't be read are left out of the returned list.
func (l *Library) write(
  ctx context.Context, dir string, docs []Doc, report Progress,
) ([]Doc, error) {
  textF, err := os.Create(dir + "/text.bin")
  if err != nil {
    return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
  }

  defer textF.Close()

  text := bufio.NewWriterSize(textF, 1 << 20)
  var (
    chunks []chunk
    kept   []Doc
    off    uint64
    words  uint64
    terms  = make(map[string]*postings, 1 << 16)
    counts = make(map[string]uint16, 1024)
    buf    = make([]byte, 0, 64)
  )

  for i, d := range docs {
    if ctx.Err() != nil {
      return nil, fmt.Errorf("\n\t... in write:\n\t%w", ctx.Err())
    }

    pieces, ok := l.readCache(d)
    if !ok {
      continue
    }

    docID := uint32(len(kept))
    kept = append(kept, d)
    for _, p := range pieces {
      id := uint32(len(chunks))
      n := uint32(0)
      clear(counts)
      count := func(t []byte) {
        n++
        c := counts[string(t)] // no allocation: map lookup by []byte
        if c < 65535 {
          counts[string(t)] = c + 1
        }
      }

      eachTerm(p.Label, buf, count)
      eachTerm(p.Text, buf, count)
      for t, c := range counts {
        ps := terms[t]
        if ps == nil {
          ps = &postings{}
          terms[t] = ps
        }

        ps.chunks = append(ps.chunks, id)
        ps.tfs = append(ps.tfs, c)
      }

      rec := p.Label + "\x1f" + p.Text
      _, err := text.WriteString(rec)
      if err != nil {
        return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
      }

      chunks = append(chunks, chunk{
        Doc: docID, Loc: p.Loc, Off: off, Len: uint32(len(rec)), Words: n,
      })
      off += uint64(len(rec))
      words += uint64(n)
    }

    if i % 50 == 0 {
      report(i, len(docs), "indexing")
    }
  }

  err = text.Flush()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
  }

  m := meta{ Version: indexVersion, Docs: kept }
  if len(chunks) > 0 {
    m.AvgLen = float64(words) / float64(len(chunks))
  }

  err = writeChunks(dir + "/chunks.bin", chunks)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
  }

  m.Sparse, m.TermsAt, err = writeTerms(dir, terms)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
  }

  f, err := os.Create(dir + "/meta.gob")
  if err != nil {
    return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
  }

  err = gob.NewEncoder(f).Encode(m)
  f.Close()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in write:\n\t%w", err)
  }

  report(len(docs), len(docs), "indexed")

  return kept, nil
}

func writeChunks(path string, chunks []chunk) error {
  f, err := os.Create(path)
  if err != nil {
    return fmt.Errorf("\n\t... in writeChunks:\n\t%w", err)
  }

  w := bufio.NewWriter(f)
  var rec [chunkSize]byte
  for _, c := range chunks {
    binary.LittleEndian.PutUint32(rec[0:], c.Doc)
    binary.LittleEndian.PutUint32(rec[4:], c.Loc)
    binary.LittleEndian.PutUint64(rec[8:], c.Off)
    binary.LittleEndian.PutUint32(rec[16:], c.Len)
    binary.LittleEndian.PutUint32(rec[20:], c.Words)
    w.Write(rec[:])
  }

  err = w.Flush()
  if err != nil {
    f.Close()
    return fmt.Errorf("\n\t... in writeChunks:\n\t%w", err)
  }

  return f.Close()
}

// writeTerms writes the dictionary (terms.bin: sorted records of
// len u8, term, postings offset u64, length u32, df u32) and the
// postings (post.bin: varint chunk deltas and counts).
func writeTerms(
  dir string, terms map[string]*postings,
) ([]sparseTerm, int64, error) {
  keys := make([]string, 0, len(terms))
  for t := range terms {
    keys = append(keys, t)
  }

  sort.Strings(keys)

  tf, err := os.Create(dir + "/terms.bin")
  if err != nil {
    return nil, 0, fmt.Errorf("\n\t... in writeTerms:\n\t%w", err)
  }

  defer tf.Close()

  pf, err := os.Create(dir + "/post.bin")
  if err != nil {
    return nil, 0, fmt.Errorf("\n\t... in writeTerms:\n\t%w", err)
  }

  defer pf.Close()

  tw := bufio.NewWriterSize(tf, 1 << 20)
  pw := bufio.NewWriterSize(pf, 1 << 20)
  var (
    sparse       []sparseTerm
    tOff, pOff   int64
    rec          = make([]byte, 0, 64)
    vb           [binary.MaxVarintLen64]byte
  )

  for i, t := range keys {
    if i % sparseEvery == 0 {
      sparse = append(sparse, sparseTerm{ Term: t, Off: tOff })
    }

    ps := terms[t]
    start := pOff
    prev := uint32(0)
    for j, c := range ps.chunks {
      n := binary.PutUvarint(vb[:], uint64(c - prev))
      pw.Write(vb[:n])
      pOff += int64(n)
      n = binary.PutUvarint(vb[:], uint64(ps.tfs[j]))
      pw.Write(vb[:n])
      pOff += int64(n)
      prev = c
    }

    rec = rec[:0]
    rec = append(rec, byte(len(t)))
    rec = append(rec, t...)
    rec = binary.LittleEndian.AppendUint64(rec, uint64(start))
    rec = binary.LittleEndian.AppendUint32(rec, uint32(pOff - start))
    rec = binary.LittleEndian.AppendUint32(rec, uint32(len(ps.chunks)))
    _, err := tw.Write(rec)
    if err != nil {
      return nil, 0, fmt.Errorf("\n\t... in writeTerms:\n\t%w", err)
    }

    tOff += int64(len(rec))
  }

  err = tw.Flush()
  if err == nil {
    err = pw.Flush()
  }

  return sparse, tOff, err
}

// load opens the index on disk, nil if there is none.
func (l *Library) load() (*index, error) {
  dir := l.dir("index")
  f, err := os.Open(dir + "/meta.gob")
  if os.IsNotExist(err) {
    return nil, nil
  }

  if err != nil {
    return nil, fmt.Errorf("\n\t... in load:\n\t%w", err)
  }

  var m meta
  err = gob.NewDecoder(bufio.NewReader(f)).Decode(&m)
  f.Close()
  if err != nil || m.Version != indexVersion {
    return nil, fmt.Errorf(
      "\n\t... in load:\n\t unreadable index, rebuild it (/library index)",
    )
  }

  raw, err := os.ReadFile(dir + "/chunks.bin")
  if err != nil {
    return nil, fmt.Errorf("\n\t... in load:\n\t%w", err)
  }

  chunks := make([]chunk, len(raw) / chunkSize)
  for i := range chunks {
    rec := raw[i * chunkSize:]
    chunks[i] = chunk{
      Doc:   binary.LittleEndian.Uint32(rec[0:]),
      Loc:   binary.LittleEndian.Uint32(rec[4:]),
      Off:   binary.LittleEndian.Uint64(rec[8:]),
      Len:   binary.LittleEndian.Uint32(rec[16:]),
      Words: binary.LittleEndian.Uint32(rec[20:]),
    }
  }

  idx := &index{ meta: m, chunks: chunks }
  files := []indexFile{
    { &idx.terms, "terms.bin" },
    { &idx.post, "post.bin" },
    { &idx.text, "text.bin" },
  }

  for _, x := range files {
    *x.f, err = os.Open(dir + "/" + x.name)
    if err != nil {
      idx.close()
      return nil, fmt.Errorf("\n\t... in load:\n\t%w", err)
    }
  }

  return idx, nil
}

func (idx *index) close() {
  if idx == nil {
    return
  }

  for _, f := range []*os.File{ idx.terms, idx.post, idx.text } {
    if f != nil {
      f.Close()
    }
  }
}

// current is the loaded index, loading it on first use.
func (l *Library) current() (*index, error) {
  l.mu.Lock()
  defer l.mu.Unlock()

  if l.idx != nil {
    return l.idx, nil
  }

  idx, err := l.load()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in current:\n\t%w", err)
  }

  l.idx = idx

  return idx, nil
}

// lookup finds a term's postings: binary search over the sparse
// marks, then a scan of one block of terms.bin.
func (idx *index) lookup(
  term string,
) (off int64, size, df uint32, ok bool) {
  sp := idx.meta.Sparse
  i := sort.Search(len(sp), func(i int) bool { return sp[i].Term > term }) - 1
  if i < 0 {
    return 0, 0, 0, false
  }

  end := idx.meta.TermsAt
  if i + 1 < len(sp) {
    end = sp[i + 1].Off
  }

  block := make([]byte, end - sp[i].Off)
  _, err := idx.terms.ReadAt(block, sp[i].Off)
  if err != nil && err != io.EOF {
    return 0, 0, 0, false
  }

  for len(block) > 0 {
    n := int(block[0])
    if len(block) < 1 + n + 16 {
      break
    }

    t := string(block[1:1 + n])
    rest := block[1 + n:]
    if t == term {
      return int64(binary.LittleEndian.Uint64(rest)),
        binary.LittleEndian.Uint32(rest[8:]),
        binary.LittleEndian.Uint32(rest[12:]), true
    }

    if t > term {
      break
    }

    block = rest[16:]
  }

  return 0, 0, 0, false
}

// readText returns a chunk's label and text.
func (idx *index) readText(c chunk) (string, string) {
  buf := make([]byte, c.Len)
  idx.text.ReadAt(buf, int64(c.Off))
  s := string(buf)
  label, text, _ := strings.Cut(s, "\x1f")

  return label, text
}
