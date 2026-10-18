package library

import (
  "encoding/binary"
  "fmt"
  "math"
  "sort"
  "strings"
)

// perDoc caps passages from one document, so a big book can't
// fill the list; snippetLen is how much of each passage is shown.
const (
  DefaultCount = 5
  MaxCount     = 10
  perDoc       = 2
  snippetLen   = 700
  bm25K1       = 1.2
  bm25B        = 0.75
)

// Search ranks passages by BM25 over the query's terms, favoring
// passages that hold more of them. topic, when set, keeps to the
// books the topic table lists for it (or to a folder of that name).
func (l *Library) Search(
  query string, count int, topic string,
) ([]Result, error) {
  if count <= 0 {
    count = DefaultCount
  }

  count = min(count, MaxCount)

  idx, err := l.current()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Search:\n\t%w", err)
  }

  if idx == nil {
    l.mu.Lock()
    b := l.building
    l.mu.Unlock()
    if b != "" {
      return nil, fmt.Errorf(
        "\n\t... in Search:\n\t the library is being indexed (%s); "+
          "try again in a minute, or use web_search meanwhile",
        b,
      )
    }

    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t the library isn't indexed yet; " +
        "the user can run /library index",
    )
  }

  terms := queryTerms(query)
  if len(terms) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t the query has no searchable words",
    )
  }

  allowed := l.allowedDocs(idx, topic)
  if allowed != nil && len(allowed) == 0 {
    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t no books for topic %q; "+
        "library_topics lists the topics",
      topic,
    )
  }

  scores := make(map[uint32]float64, 1024)
  hits := make(map[uint32]uint8, 1024)
  n := float64(len(idx.chunks))
  for _, t := range terms {
    off, size, df, ok := idx.lookup(t)
    if !ok {
      continue
    }

    idf := math.Log(1 + (n - float64(df) + 0.5) / (float64(df) + 0.5))
    raw := make([]byte, size)
    _, err := idx.post.ReadAt(raw, off)
    if err != nil {
      continue
    }

    c := uint32(0)
    for len(raw) > 0 {
      d, k := binary.Uvarint(raw)
      if k <= 0 {
        break
      }

      raw = raw[k:]
      tf, k := binary.Uvarint(raw)
      if k <= 0 {
        break
      }

      raw = raw[k:]
      c += uint32(d)
      if allowed != nil && !allowed[idx.chunks[c].Doc] {
        continue
      }

      length := float64(idx.chunks[c].Words) / idx.meta.AvgLen
      f := float64(tf)
      norm := bm25K1 * (1 - bm25B + bm25B * length)
      scores[c] += idf * f * (bm25K1 + 1) / (f + norm)
      hits[c]++
    }
  }

  type scored struct {
    c     uint32
    score float64
  }

  ranked := make([]scored, 0, len(scores))
  for c, s := range scores {
    cover := float64(hits[c]) / float64(len(terms))
    ranked = append(ranked, scored{ c, s * (0.4 + 0.6 * cover * cover) })
  }

  sort.Slice(ranked, func(i, j int) bool {
    if ranked[i].score != ranked[j].score {
      return ranked[i].score > ranked[j].score
    }

    return ranked[i].c < ranked[j].c
  })

  var out []Result
  per := map[uint32]int{}
  for _, r := range ranked {
    if len(out) == count {
      break
    }

    ch := idx.chunks[r.c]
    if per[ch.Doc] == perDoc {
      continue
    }

    per[ch.Doc]++
    out = append(out, l.result(idx, ch, terms, len(out) + 1))
  }

  return out, nil
}

// allowedDocs is the documents a topic keeps to; nil means all.
func (l *Library) allowedDocs(idx *index, topic string) map[uint32]bool {
  topic = strings.ToLower(strings.TrimSpace(topic))
  if topic == "" {
    return nil
  }

  books := map[string]bool{}
  for _, t := range l.Topics() {
    if strings.Contains(t.Topic, topic) {
      books[t.Book] = true
    }
  }

  allowed := map[uint32]bool{}
  for i, d := range idx.meta.Docs {
    folder := strings.HasPrefix(strings.ToLower(d.Path), topic + "/")
    if books[d.Book] || folder {
      allowed[uint32(i)] = true
    }
  }

  return allowed
}

// result turns a chunk into what the model sees, with where it is
// and how to read on.
func (l *Library) result(idx *index, ch chunk, terms []string, id int) Result {
  d := idx.meta.Docs[ch.Doc]
  label, text := idx.readText(ch)
  abs := l.Abs(d.Path)
  r := Result{
    ID:      id,
    Title:   docTitle(d),
    File:    abs,
    Section: label,
    Text:    snippet(text, terms),
  }

  if d.Kind == kindPDF {
    r.Where = fmt.Sprintf("page %d", ch.Loc)
    r.ReadMore = fmt.Sprintf(
      `read_pdf {"path": %q, "first_page": %d, "last_page": %d}`,
      abs, ch.Loc, ch.Loc + 1,
    )
  } else {
    r.Where = fmt.Sprintf("line %d", ch.Loc)
    r.ReadMore = fmt.Sprintf(
      `read_file {"path": %q, "start_line": %d, "end_line": %d}`,
      abs, ch.Loc, ch.Loc + 80,
    )
  }

  return r
}

// snippet is the snippetLen window of text holding the most query
// terms, cut at spaces.
func snippet(text string, terms []string) string {
  if len(text) <= snippetLen {
    return text
  }

  lower := strings.ToLower(text)
  var at []int
  for _, t := range terms {
    for i := 0; ; {
      j := strings.Index(lower[i:], t)
      if j < 0 {
        break
      }

      at = append(at, i + j)
      i += j + len(t)
    }
  }

  sort.Ints(at)
  best, most := 0, 0
  for i, p := range at {
    n := 0
    for _, q := range at[i:] {
      if q - p > snippetLen - 150 {
        break
      }

      n++
    }

    if n > most {
      best, most = p, n
    }
  }

  start := max(0, best - 150)
  end := min(len(text), start + snippetLen)
  start = max(0, end - snippetLen)
  if start > 0 {
    sp := strings.IndexByte(text[start:], ' ')
    if sp >= 0 && sp < 40 {
      start += sp + 1
    }
  }

  if end < len(text) {
    sp := strings.LastIndexByte(text[start:end], ' ')
    if sp > snippetLen / 2 {
      end = start + sp
    }
  }

  s := text[start:end]
  if start > 0 {
    s = "…" + s
  }

  if end < len(text) {
    s += "…"
  }

  return s
}
