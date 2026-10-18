package library

import (
  "context"
  "fmt"
  "os"
  "path"
  "sort"
  "strings"
)

const (
  maxContents = 7000 // characters of a book's contents sent to the model
  maxRows     = 20   // topic rows kept per book
)

// seedTopics steer the catalog toward one shared vocabulary; the
// model adds others when nothing fits.
var seedTopics = []string{
  "go", "c", "c-standard-library", "system-calls", "linux-commands",
  "shell-scripting", "git", "build-tools", "debugging", "operating-systems",
  "processes-threads", "concurrency", "synchronization", "memory-management",
  "virtual-memory", "file-systems", "networking", "sockets", "http", "tcp-ip",
  "tls", "dns", "distributed-systems", "system-design", "backend", "web-apis",
  "databases", "sql", "indexing", "transactions", "frontend", "html", "css",
  "javascript", "web-apis-browser", "security", "web-security", "cryptography",
  "data-structures", "algorithms", "graphs", "complexity", "performance",
  "cpu-caches", "simd", "profiling", "computer-architecture", "assembly",
  "x86", "risc-v", "digital-logic", "electronics", "embedded", "rtos",
  "device-drivers", "compilers", "interpreters", "parsing", "calculus",
  "linear-algebra", "discrete-math", "logic", "proofs", "set-theory",
  "probability", "statistics", "number-theory", "abstract-algebra",
  "theory-of-computation", "information-theory", "numerical-methods",
  "mechanics", "analytical-mechanics", "electromagnetism", "thermodynamics",
  "optics", "quantum", "simulation", "machine-learning", "deep-learning",
  "signal-processing", "computer-graphics",
}

const catalogSystem = `You catalog technical references for a coding assistant's library. From a book's contents, list the topics it covers and exactly where to read about each.

Reply with lines only, no other text, one per topic:
topic | where | note

- topic: lowercase-kebab-case. Reuse a known topic when one fits; add a new one only for a real gap.
- where: chapter or section numbers and names, with the printed page ranges when the contents show them ("ch. 26-32 Concurrency, pp. 309-410"). For a folder, the files or subfolders ("reference/global_objects/promise/").
- note: at most 12 words on what this book is good for there.
- 3 to 15 lines. Only topics the book really covers in depth.`

// signature changes when any of a book's files does.
func signature(docs []Doc) string {
  var size, mtime int64
  for _, d := range docs {
    size += d.Size
    mtime = max(mtime, d.Mtime)
  }

  return fmt.Sprintf("%d:%d:%d", len(docs), size, mtime)
}

// byBook groups documents by book.
func byBook(docs []Doc) (map[string][]Doc, []string) {
  books := map[string][]Doc{}
  var names []string
  for _, d := range docs {
    if books[d.Book] == nil {
      names = append(names, d.Book)
    }

    books[d.Book] = append(books[d.Book], d)
  }

  return books, names
}

// cataloged reads .armin/cataloged.tsv: book → signature.
func (l *Library) cataloged() map[string]string {
  data, _ := os.ReadFile(l.dir("cataloged.tsv"))
  m := map[string]string{}
  for _, line := range strings.Split(string(data), "\n") {
    book, sig, ok := strings.Cut(line, "\t")
    if ok {
      m[book] = sig
    }
  }

  return m
}

func (l *Library) saveCataloged(m map[string]string) error {
  keys := make([]string, 0, len(m))
  for k := range m {
    keys = append(keys, k)
  }

  sort.Strings(keys)
  var b strings.Builder
  for _, k := range keys {
    fmt.Fprintf(&b, "%s\t%s\n", k, m[k])
  }

  return os.WriteFile(l.dir("cataloged.tsv"), []byte(b.String()), 0o644)
}

// Pending lists the books not in the topic table yet, or changed
// since they were cataloged. docs nil scans the library.
func (l *Library) Pending(docs []Doc) []string {
  if docs == nil {
    docs, _ = l.scan()
  }

  done := l.cataloged()
  books, names := byBook(docs)
  var out []string
  for _, b := range names {
    if done[b] != signature(books[b]) {
      out = append(out, b)
    }
  }

  return out
}

// Catalog has a model read the contents of each pending book and
// adds its topic rows to the table, saving after every book so an
// interrupted run keeps what it did. The index should be built
// first: contents come from the extraction cache.
func (l *Library) Catalog(
  ctx context.Context, ask Ask, progress Progress,
) (int, error) {
  docs, err := l.scan()
  if err != nil {
    return 0, fmt.Errorf("\n\t... in Catalog:\n\t%w", err)
  }

  pending := l.Pending(docs)
  books, _ := byBook(docs)
  done := l.cataloged()
  known := l.knownTopics()
  aliases := l.aliases()
  added := 0

  for i, book := range pending {
    if ctx.Err() != nil {
      return added, ctx.Err()
    }

    if progress != nil {
      progress(i, len(pending), book)
    }

    user := fmt.Sprintf(
      "Book: %s\nPath: %s\nKnown topics: %s\n\nContents:\n%s",
      BookTitle(book),
      book,
      strings.Join(known, ", "),
      l.contents(books[book]),
    )
    reply, err := ask(ctx, catalogSystem, user)
    if err != nil {
      return added, fmt.Errorf("\n\t... in Catalog:\n\t %s: %w", book, err)
    }

    rows := parseRows(reply, book)
    if len(rows) == 0 {
      continue // try again next time
    }

    // names merged earlier keep their canonical form
    for i := range rows {
      rows[i].Topic = canonical(aliases, rows[i].Topic)
    }

    rows = mergeRows(rows, nil)

    // replace what an earlier catalog said about this book
    var keep []Topic
    for _, t := range l.Topics() {
      if t.Book != book {
        keep = append(keep, t)
      }
    }

    err = l.saveTopics(append(keep, rows...))
    if err != nil {
      return added, fmt.Errorf("\n\t... in Catalog:\n\t%w", err)
    }

    done[book] = signature(books[book])
    err = l.saveCataloged(done)
    if err != nil {
      return added, fmt.Errorf("\n\t... in Catalog:\n\t%w", err)
    }

    for _, r := range rows {
      known = appendNew(known, r.Topic)
    }

    added += len(rows)
  }

  if progress != nil {
    progress(len(pending), len(pending), "cataloged")
  }

  return added, nil
}

func (l *Library) knownTopics() []string {
  known := append([]string(nil), seedTopics...)
  for _, t := range l.Topics() {
    known = appendNew(known, t.Topic)
  }

  return known
}

func appendNew(list []string, s string) []string {
  for _, x := range list {
    if x == s {
      return list
    }
  }

  return append(list, s)
}

// parseRows reads "topic | where | note" lines, skipping anything
// else the model said.
func parseRows(reply, book string) []Topic {
  var rows []Topic
  for _, line := range strings.Split(reply, "\n") {
    line = strings.Trim(strings.TrimSpace(line), "-*` ")
    f := strings.Split(line, "|")
    if len(f) < 2 {
      continue
    }

    topic := kebab(f[0])
    where := strings.TrimSpace(f[1])
    empty := where == "" || strings.Trim(where, "-: ") == ""
    if topic == "" || topic == "topic" || empty {
      continue
    }

    t := Topic{ Topic: topic, Book: book, Where: where }
    if len(f) > 2 {
      t.Note = strings.TrimSpace(strings.Join(f[2:], " "))
    }

    rows = append(rows, t)
    if len(rows) == maxRows {
      break
    }
  }

  return rows
}

func kebab(s string) string {
  var b strings.Builder
  dash := false
  for _, r := range strings.ToLower(strings.TrimSpace(s)) {
    switch {
    case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
      if dash && b.Len() > 0 {
        b.WriteByte('-')
      }

      b.WriteRune(r)
      dash = false
    default:
      dash = true
    }
  }

  return b.String()
}

// contents is what the model reads to catalog a book: for a single
// document its table of contents (or its headings), for a folder
// its files with their first headings, or its subfolders when
// there are too many files.
func (l *Library) contents(docs []Doc) string {
  if len(docs) == 1 {
    return l.docContents(docs[0])
  }

  book := docs[0].Book
  var b strings.Builder
  if len(docs) <= 150 {
    for _, d := range docs {
      fmt.Fprintf(&b, "%s", strings.TrimPrefix(d.Path, book + "/"))
      pieces, ok := l.readCache(d)
      if ok && len(pieces) > 0 {
        h := pieces[0].Label
        if h == "" {
          h = pieces[0].Text
        }

        fmt.Fprintf(&b, ": %s", truncate(h, 80))
      }

      b.WriteByte('\n')
      if b.Len() > maxContents {
        break
      }
    }

    return truncate(b.String(), maxContents)
  }

  count := map[string]int{}
  var dirs []string
  for _, d := range docs {
    rel := strings.TrimPrefix(d.Path, book + "/")
    dir := path.Dir(rel)
    parts := strings.Split(dir, "/")
    if len(parts) > 3 {
      dir = strings.Join(parts[:3], "/")
    }

    if count[dir] == 0 {
      dirs = append(dirs, dir)
    }

    count[dir]++
  }

  fmt.Fprintf(&b, "%d files in these folders:\n", len(docs))
  for _, d := range dirs {
    fmt.Fprintf(&b, "%s/ (%d)\n", d, count[d])
  }

  return truncate(b.String(), maxContents)
}

// docContents is a document's table of contents: for a PDF, its
// first pages from the word "Contents"; otherwise its headings.
func (l *Library) docContents(d Doc) string {
  pieces, _ := l.readCache(d)
  if d.Kind == kindPDF {
    var b strings.Builder
    for _, p := range pieces {
      if p.Loc > 30 || b.Len() > maxContents * 2 {
        break
      }

      fmt.Fprintf(&b, "[page %d] %s\n", p.Loc, p.Text)
    }

    s := b.String()
    i := strings.Index(strings.ToLower(s), "contents")
    if i > 0 {
      s = s[i:]
    }

    return fmt.Sprintf(
      "(%d pages; printed page numbers may differ from file pages)\n%s",
      len(pieces),
      truncate(s, maxContents),
    )
  }

  var b strings.Builder
  seen := map[string]bool{}
  for _, p := range pieces {
    if p.Label != "" && !seen[p.Label] {
      seen[p.Label] = true
      fmt.Fprintf(&b, "line %d: %s\n", p.Loc, truncate(p.Label, 100))
    }
  }

  if b.Len() == 0 && len(pieces) > 0 {
    b.WriteString(pieces[0].Text)
  }

  return truncate(b.String(), maxContents)
}

func truncate(s string, n int) string {
  if len(s) <= n {
    return s
  }

  return s[:n] + "…"
}
