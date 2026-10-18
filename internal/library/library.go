// Package library is armin's reference shelf: a folder of books,
// docs, standards and man pages (library = <dir> in config.conf),
// searched page by page and section by section, plus a topic table
// a model writes so the agent knows where each subject is covered.
//
// Everything armin keeps lives in <root>/.armin: the extraction
// cache, the index and the topic table. Top-level folders are
// topics (go, c, math, ...); each file or folder inside one is a
// book.
package library

import (
  "fmt"
  "io"
  "io/fs"
  "os"
  "path/filepath"
  "sort"
  "strings"
)

const (
  kindText Kind = iota
  kindPDF
  kindHTML
  kindMarkdown
)

// maxDocSize skips anything bigger: no reference is that large, a
// video or disk image is.
const maxDocSize = 512 << 20

// kinds maps a file extension to how it's read; others are skipped.
var kinds = map[string]Kind{
  ".pdf":      kindPDF,
  ".html":     kindHTML,
  ".htm":      kindHTML,
  ".xhtml":    kindHTML,
  ".md":       kindMarkdown,
  ".mdx":      kindMarkdown,
  ".markdown": kindMarkdown,
  ".rst":      kindMarkdown,
  ".adoc":     kindMarkdown,
  ".txt":      kindText,
  ".text":     kindText,
  ".tex":      kindText,
  ".go":       kindText,
  ".c":        kindText,
  ".h":        kindText,
  ".py":       kindText,
  ".js":       kindText,
  ".sh":       kindText,
}

// Open returns the library at root, creating its .armin folder.
// Nothing is read until the first search or Build.
func Open(root string) (*Library, error) {
  abs, err := filepath.Abs(expandHome(root))
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  info, err := os.Stat(abs)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  if !info.IsDir() {
    return nil, fmt.Errorf("\n\t... in Open:\n\t %s isn't a directory", abs)
  }

  err = os.MkdirAll(filepath.Join(abs, ".armin", "cache"), 0o755)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  return &Library{ Root: abs }, nil
}

func expandHome(p string) string {
  rest, ok := strings.CutPrefix(p, "~")
  if !ok {
    return p
  }

  home, err := os.UserHomeDir()
  if err != nil {
    return p
  }

  return home + rest
}

// dir is a path inside .armin.
func (l *Library) dir(parts ...string) string {
  return filepath.Join(append([]string{ l.Root, ".armin" }, parts...)...)
}

// Abs turns a library path into an absolute one.
func (l *Library) Abs(rel string) string {
  return filepath.Join(l.Root, filepath.FromSlash(rel))
}

// scan lists every readable document, sorted by path. Dot files and
// folders (.armin, .git) and TOPICS.md are skipped.
func (l *Library) scan() ([]Doc, error) {
  var docs []Doc
  walk := func(p string, d fs.DirEntry, err error) error {
    if err != nil {
      return nil
    }

    name := d.Name()
    if p != l.Root && strings.HasPrefix(name, ".") {
      if d.IsDir() {
        return filepath.SkipDir
      }

      return nil
    }

    if d.IsDir() {
      return nil
    }

    kind, ok := kinds[strings.ToLower(filepath.Ext(name))]
    if !ok {
      return nil
    }

    rel, _ := filepath.Rel(l.Root, p)
    rel = filepath.ToSlash(rel)
    if rel == "TOPICS.md" {
      return nil
    }

    info, err := d.Info()
    if err != nil || info.Size() == 0 || info.Size() > maxDocSize {
      return nil
    }

    docs = append(docs, Doc{
      Path:  rel,
      Book:  bookOf(rel),
      Size:  info.Size(),
      Mtime: info.ModTime().Unix(),
      Kind:  kind,
    })

    return nil
  }

  err := filepath.WalkDir(l.Root, walk)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in scan:\n\t%w", err)
  }

  sort.Slice(docs, func(i, j int) bool { return docs[i].Path < docs[j].Path })

  return docs, nil
}

// bookOf is the unit a document belongs to: the file or folder
// right inside its topic folder ("os/OSTEP/vm.pdf" → "os/OSTEP"), or
// the file itself at the top level.
func bookOf(rel string) string {
  first := strings.IndexByte(rel, '/')
  if first < 0 {
    return rel
  }

  second := strings.IndexByte(rel[first + 1:], '/')
  if second < 0 {
    return rel
  }

  return rel[:first + 1 + second]
}

// BookTitle is a book's name without folder or extension.
func BookTitle(book string) string {
  name := book[strings.LastIndexByte(book, '/') + 1:]
  ext := filepath.Ext(name)
  _, ok := kinds[strings.ToLower(ext)]
  if ok {
    name = strings.TrimSuffix(name, ext)
  }

  return name
}

// docTitle is how a result names its document: the book, then the
// file inside it when the book is a folder.
func docTitle(d Doc) string {
  t := BookTitle(d.Book)
  if d.Path == d.Book {
    return t
  }

  inner := strings.TrimPrefix(d.Path, d.Book + "/")
  ext := filepath.Ext(inner)
  _, known := kinds[strings.ToLower(ext)]
  if known {
    inner = strings.TrimSuffix(inner, ext)
  }

  return t + " › " + inner
}

// Add copies files or folders into the library, under dir (a topic
// folder, created if needed; "added" when empty). It returns the
// library paths it wrote. Nothing is indexed until Build.
func (l *Library) Add(paths []string, dir string) ([]string, error) {
  if dir == "" {
    dir = "added"
  }

  dir = filepath.Clean(dir)
  if filepath.IsAbs(dir) || strings.HasPrefix(dir, ".") {
    return nil, fmt.Errorf(
      "\n\t... in Add:\n\t %q must be a folder name inside the library",
      dir,
    )
  }

  var added []string
  for _, src := range paths {
    src = expandHome(src)
    info, err := os.Stat(src)
    if err != nil {
      return added, fmt.Errorf("\n\t... in Add:\n\t%w", err)
    }

    rel := filepath.ToSlash(filepath.Join(dir, filepath.Base(src)))
    dst := l.Abs(rel)
    if info.IsDir() {
      err = copyTree(src, dst)
    } else {
      err = copyFile(src, dst)
    }

    if err != nil {
      return added, fmt.Errorf("\n\t... in Add:\n\t%w", err)
    }

    added = append(added, rel)
  }

  return added, nil
}

func copyTree(src, dst string) error {
  err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
    if err != nil {
      return err
    }

    rel, _ := filepath.Rel(src, p)
    target := filepath.Join(dst, rel)
    if d.IsDir() {
      if d.Name() == ".git" {
        return filepath.SkipDir
      }

      return os.MkdirAll(target, 0o755)
    }

    return copyFile(p, target)
  })
  if err != nil {
    return fmt.Errorf("\n\t... in copyTree:\n\t%w", err)
  }

  return nil
}

func copyFile(src, dst string) error {
  err := os.MkdirAll(filepath.Dir(dst), 0o755)
  if err != nil {
    return fmt.Errorf("\n\t... in copyFile:\n\t%w", err)
  }

  in, err := os.Open(src)
  if err != nil {
    return fmt.Errorf("\n\t... in copyFile:\n\t%w", err)
  }

  defer in.Close()

  out, err := os.Create(dst)
  if err != nil {
    return fmt.Errorf("\n\t... in copyFile:\n\t%w", err)
  }

  _, err = io.Copy(out, in)
  if err != nil {
    out.Close()
    return fmt.Errorf("\n\t... in copyFile:\n\t %s: %w", src, err)
  }

  err = out.Close()
  if err != nil {
    return fmt.Errorf("\n\t... in copyFile:\n\t%w", err)
  }

  return nil
}

// Status reports the index and topic table.
func (l *Library) Status() Status {
  s := Status{ Root: l.Root }
  docs, _ := l.scan()
  s.Docs = len(docs)

  books := map[string]bool{}
  for _, d := range docs {
    books[d.Book] = true
  }

  s.Books = len(books)

  l.mu.Lock()
  s.Building = l.building
  idx := l.idx
  l.mu.Unlock()

  if idx == nil {
    idx, _ = l.load()
  }

  if idx != nil {
    s.Chunks = len(idx.chunks)
    s.Stale = staleCount(idx.meta.Docs, docs)
  } else {
    s.Stale = len(docs)
  }

  s.Topics = len(l.Topics())
  s.Pending = len(l.Pending(docs))

  return s
}

// staleCount is how many documents were added, changed or removed
// since the index was built.
func staleCount(indexed, now []Doc) int {
  old := make(map[string]Doc, len(indexed))
  for _, d := range indexed {
    old[d.Path] = d
  }

  n := 0
  for _, d := range now {
    o, ok := old[d.Path]
    if !ok || o.Size != d.Size || o.Mtime != d.Mtime {
      n++
    }

    delete(old, d.Path)
  }

  return n + len(old)
}

// Stale reports whether the index is missing or out of date.
func (l *Library) Stale() bool {
  docs, err := l.scan()
  if err != nil {
    return false
  }

  idx, err := l.current()
  if err != nil || idx == nil {
    return len(docs) > 0
  }

  return staleCount(idx.meta.Docs, docs) > 0
}
