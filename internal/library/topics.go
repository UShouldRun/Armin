package library

import (
  "bufio"
  "fmt"
  "os"
  "sort"
  "strings"
)

const (
  // promptTopics and promptBooks bound the "where to look" lines in
  // the system prompt; the full table is one library_topics away.
  promptTopics = 60
  promptBooks  = 3
)

// Topics is the topic table (.armin/topics.tsv), reloaded when the
// file changes.
func (l *Library) Topics() []Topic {
  path := l.dir("topics.tsv")
  info, err := os.Stat(path)
  if err != nil {
    return nil
  }

  l.mu.Lock()
  defer l.mu.Unlock()

  if l.topics != nil && info.ModTime().UnixNano() == l.topicsAt {
    return l.topics
  }

  f, err := os.Open(path)
  if err != nil {
    return nil
  }

  defer f.Close()

  var rows []Topic
  sc := bufio.NewScanner(f)
  sc.Buffer(make([]byte, 0, 64 << 10), 1 << 20)
  for sc.Scan() {
    line := sc.Text()
    if line == "" || strings.HasPrefix(line, "#") {
      continue
    }

    f := strings.Split(line, "\t")
    if len(f) < 3 {
      continue
    }

    t := Topic{ Topic: f[0], Book: f[1], Where: f[2] }
    if len(f) > 3 {
      t.Note = f[3]
    }

    rows = append(rows, t)
  }

  l.topics, l.topicsAt = rows, info.ModTime().UnixNano()

  return rows
}

// saveTopics rewrites topics.tsv (sorted by topic, then book) and
// TOPICS.md, the same table for people.
func (l *Library) saveTopics(rows []Topic) error {
  sort.SliceStable(rows, func(i, j int) bool {
    if rows[i].Topic != rows[j].Topic {
      return rows[i].Topic < rows[j].Topic
    }

    return rows[i].Book < rows[j].Book
  })

  var b strings.Builder
  b.WriteString("# topic\tbook\twhere\tnote — written by armin's ")
  b.WriteString("/library catalog; hand edits are kept\n")
  for _, t := range rows {
    fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n",
      t.Topic, t.Book, clean(t.Where), clean(t.Note))
  }

  path := l.dir("topics.tsv")
  err := os.WriteFile(path + ".tmp", []byte(b.String()), 0o644)
  if err != nil {
    return fmt.Errorf("\n\t... in saveTopics:\n\t%w", err)
  }

  err = os.Rename(path + ".tmp", path)
  if err != nil {
    return fmt.Errorf("\n\t... in saveTopics:\n\t%w", err)
  }

  // mtimes are too coarse to tell two quick saves apart
  l.mu.Lock()
  l.topics = nil
  l.mu.Unlock()

  return os.WriteFile(l.Abs("TOPICS.md"), []byte(topicsMarkdown(rows)), 0o644)
}

func clean(s string) string {
  return strings.Join(strings.Fields(strings.ReplaceAll(s, "\t", " ")), " ")
}

func topicsMarkdown(rows []Topic) string {
  var b strings.Builder
  b.WriteString("# Where to look\n\n")
  b.WriteString("Written by armin's /library catalog: which book covers what. ")
  b.WriteString("armin's library_topics tool reads the same table.\n")
  last := ""
  for _, t := range rows {
    if t.Topic != last {
      fmt.Fprintf(&b, "\n## %s\n\n", t.Topic)
      last = t.Topic
    }

    fmt.Fprintf(&b, "- **%s**: %s", BookTitle(t.Book), t.Where)
    if t.Note != "" {
      fmt.Fprintf(&b, " — %s", t.Note)
    }

    fmt.Fprintf(&b, " (`%s`)\n", t.Book)
  }

  return b.String()
}

// TopicRows answers library_topics: the rows of every topic whose
// name contains topic, or with topic empty, each topic and its book
// count.
func (l *Library) TopicRows(topic string) []Topic {
  rows := l.Topics()
  topic = strings.ToLower(strings.TrimSpace(topic))
  if topic != "" {
    var out []Topic
    for _, t := range rows {
      if strings.Contains(t.Topic, topic) {
        out = append(out, t)
      }
    }

    return out
  }

  books := map[string]map[string]bool{}
  var names []string
  for _, t := range rows {
    if books[t.Topic] == nil {
      books[t.Topic] = map[string]bool{}
      names = append(names, t.Topic)
    }

    books[t.Topic][t.Book] = true
  }

  out := make([]Topic, 0, len(names))
  for _, n := range names {
    where := fmt.Sprintf("%d books", len(books[n]))
    out = append(out, Topic{ Topic: n, Where: where })
  }

  return out
}

// PromptIndex is the library's part of the system prompt: how to use
// it, then per topic the books to try first. Built once per session,
// so the prompt prefix stays byte-stable.
func (l *Library) PromptIndex() string {
  var b strings.Builder
  fmt.Fprintf(&b, "Reference library at %s: books, official docs, "+
    "standards, RFCs, man pages.\n", l.Root)
  b.WriteString("- For technical facts (language rules, APIs, " +
    "syscalls, commands, protocols, algorithms, math, physics), " +
    "search it first with library_search; narrow with topic when " +
    "one fits. library_topics shows which chapters cover a topic.\n")
  b.WriteString("- Cite what you take from it inline as " +
    "[Title, page N] (or line N), and end with a Library sources " +
    "list: \"- Title, page N: /path/to/file\". The user checks " +
    "these. Use web_search for what's newer or missing.\n")

  rows := l.Topics()
  if len(rows) == 0 {
    docs, _ := l.scan()
    count := map[string]int{}
    var dirs []string
    for _, d := range docs {
      top, _, _ := strings.Cut(d.Path, "/")
      if count[top] == 0 {
        dirs = append(dirs, top)
      }

      count[top]++
    }

    b.WriteString("Folders (topics aren't cataloged yet):")
    for _, d := range dirs {
      fmt.Fprintf(&b, " %s (%d)", d, count[d])
    }

    b.WriteByte('\n')

    return b.String()
  }

  b.WriteString("Where to look:\n")
  var (
    topics []string
    books  = map[string][]string{}
  )

  for _, t := range rows {
    if books[t.Topic] == nil {
      topics = append(topics, t.Topic)
    }

    title := shortTitle(BookTitle(t.Book))
    have := false
    for _, x := range books[t.Topic] {
      have = have || x == title
    }

    if !have && len(books[t.Topic]) < promptBooks {
      books[t.Topic] = append(books[t.Topic], title)
    }
  }

  // the most covered topics, if there are too many to list
  if len(topics) > promptTopics {
    n := map[string]int{}
    for _, t := range rows {
      n[t.Topic]++
    }

    sort.SliceStable(topics, func(i, j int) bool {
      return n[topics[i]] > n[topics[j]]
    })
    topics = topics[:promptTopics]
    sort.Strings(topics)
  }

  for _, t := range topics {
    fmt.Fprintf(&b, "- %s: %s\n", t, strings.Join(books[t], "; "))
  }

  return b.String()
}

// shortTitle drops the author in parentheses and long subtitles.
func shortTitle(t string) string {
  i := strings.Index(t, " (")
  if i > 0 {
    t = t[:i]
  }

  if len(t) > 48 {
    cut := strings.LastIndexByte(t[:48], ' ')
    if cut > 20 {
      t = t[:cut] + "…"
    }
  }

  return t
}
