package library

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "testing"
  "time"
)

func writeDoc(t *testing.T, root, rel, body string) {
  t.Helper()
  p := filepath.Join(root, rel)
  err := os.MkdirAll(filepath.Dir(p), 0o755)
  if err != nil {
    t.Fatal(err)
  }

  err = os.WriteFile(p, []byte(body), 0o644)
  if err != nil {
    t.Fatal(err)
  }
}

// minimalPDF is a two-page PDF with one line of text per page.
// Offsets in its xref are wrong; poppler rebuilds them.
func minimalPDF(page1, page2 string) string {
  obj := func(n int, body string) string {
    return fmt.Sprintf("%d 0 obj\n%s\nendobj\n", n, body)
  }

  stream := func(s string) string {
    c := "BT /F1 12 Tf 72 720 Td (" + s + ") Tj ET"
    return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(c), c)
  }

  page := func(contents int) string {
    return fmt.Sprintf("<< /Type /Page /Parent 2 0 R "+
      "/MediaBox [0 0 612 792] /Contents %d 0 R "+
      "/Resources << /Font << /F1 7 0 R >> >> >>", contents)
  }

  return "%PDF-1.4\n" +
    obj(1, "<< /Type /Catalog /Pages 2 0 R >>") +
    obj(2, "<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>") +
    obj(3, page(5)) +
    obj(4, page(6)) +
    obj(5, stream(page1)) +
    obj(6, stream(page2)) +
    obj(7, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>") +
    "trailer\n<< /Root 1 0 R /Size 8 >>\n%%EOF\n"
}

func testLibrary(t *testing.T) *Library {
  t.Helper()
  root := t.TempDir()
  writeDoc(t, root, "go/Effective Go.md",
    "# Introduction\nGo is a new language.\n\n"+
      "# Concurrency\nShare memory by communicating. "+
      "Goroutines are multiplexed onto threads; "+
      "channels synchronize them.\n\n"+
      "# Errors\nError values are returned, not thrown.\n")
  writeDoc(t, root, "c/man2/open.2.txt",
    "NAME\n  open - open and possibly create a file\n"+
      "SYNOPSIS\n  int open(const char *path, int flags);\n"+
      "DESCRIPTION\n  The open() system call opens the file "+
      "specified by pathname.\n")
  writeDoc(t, root, "web/HTTP Guide.html",
    "<html><head><style>h1{color:red}</style>"+
      "<script>var x = 'channel';</script></head><body>\n"+
      "<h1 id=\"intro\">Intro</h1><p>Hypertext &amp; transfer.</p>\n"+
      "<h2>Caching</h2><p>The Cache-Control header controls "+
      "caching.</p>\n</body></html>\n")
  writeDoc(t, root, "os/OSTEP/threads-intro.md",
    "# Threads\nA thread shares the address space of its process.\n")
  writeDoc(t, root, "os/OSTEP/vm-intro.md",
    "# Address spaces\nVirtual memory gives each process an "+
      "address space.\n")
  writeDoc(t, root, ".git/ignored.md",
    "# Concurrency\nshould never be indexed\n")

  l, err := Open(root)
  if err != nil {
    t.Fatal(err)
  }

  return l
}

func TestBuildAndSearch(t *testing.T) {
  l := testLibrary(t)
  ctx := context.Background()

  _, err := l.Search("channels", 5, "")
  if err == nil || !strings.Contains(err.Error(), "isn't indexed") {
    t.Fatalf("search before Build: err = %v, want 'isn't indexed'", err)
  }

  err = l.Build(ctx, nil)
  if err != nil {
    t.Fatalf("Build: %v", err)
  }

  res, err := l.Search("goroutines channels", 5, "")
  if err != nil {
    t.Fatalf("Search: %v", err)
  }

  if len(res) == 0 || res[0].Title != "Effective Go" ||
    res[0].Section != "Concurrency" {
    t.Fatalf("Search = %+v, want Effective Go § Concurrency first", res)
  }

  if res[0].Where != "line 4" ||
    !strings.Contains(res[0].ReadMore, `"start_line": 4`) {
    t.Fatalf("result location = %q / %q, want line 4",
      res[0].Where, res[0].ReadMore)
  }

  // the script's "channel" and .git's copy aren't indexed
  for _, r := range res {
    if strings.Contains(r.File, ".git") || strings.Contains(r.Title, "HTTP") {
      t.Fatalf("unexpected result %+v", r)
    }
  }

  res, _ = l.Search("open system call", 5, "")
  if len(res) == 0 || res[0].Title != "man2 › open.2" {
    t.Fatalf("man page search = %+v", res)
  }

  res, _ = l.Search("cache-control", 5, "")
  if len(res) == 0 || res[0].Section != "Caching" ||
    !strings.Contains(res[0].Text, "Cache-Control header") {
    t.Fatalf("html search = %+v", res)
  }

  // a book folder: the result names the file inside it
  res, _ = l.Search("thread address space", 5, "")
  if len(res) == 0 || !strings.HasPrefix(res[0].Title, "OSTEP › ") {
    t.Fatalf("folder book search = %+v", res)
  }
}

func TestPDFPages(t *testing.T) {
  _, err := exec.LookPath("pdftotext")
  if err != nil {
    t.Skip("pdftotext not installed")
  }

  l := testLibrary(t)
  pdf := minimalPDF("Limits and continuity", "The derivative measures change")
  writeDoc(t, l.Root, "math/Tiny Calculus.pdf", pdf)

  err = l.Build(context.Background(), nil)
  if err != nil {
    t.Fatalf("Build: %v", err)
  }

  res, err := l.Search("derivative", 5, "")
  if err != nil || len(res) == 0 {
    t.Fatalf("Search = %+v, %v", res, err)
  }

  r := res[0]
  if r.Title != "Tiny Calculus" || r.Where != "page 2" ||
    !strings.HasPrefix(r.ReadMore, "read_pdf") {
    t.Fatalf("pdf result = %+v, want Tiny Calculus page 2", r)
  }
}

func TestStaleAndCache(t *testing.T) {
  l := testLibrary(t)
  ctx := context.Background()
  if !l.Stale() {
    t.Fatal("Stale() = false before the first Build")
  }

  err := l.Build(ctx, nil)
  if err != nil {
    t.Fatal(err)
  }

  if l.Stale() {
    t.Fatal("Stale() = true right after Build")
  }

  writeDoc(t, l.Root, "go/Go Memory Model.md",
    "# Happens before\nA send on a channel happens before the "+
      "receive completes.\n")
  if !l.Stale() {
    t.Fatal("Stale() = false after adding a document")
  }

  // the untouched documents come from the cache
  before, _ := os.ReadDir(l.dir("cache"))
  err = l.Build(ctx, nil)
  if err != nil {
    t.Fatal(err)
  }

  after, _ := os.ReadDir(l.dir("cache"))
  if len(after) != len(before) + 1 {
    t.Fatalf("cache files %d → %d, want one more",
      len(before), len(after))
  }

  res, _ := l.Search("happens before", 3, "")
  if len(res) == 0 || res[0].Title != "Go Memory Model" {
    t.Fatalf("Search after rebuild = %+v", res)
  }

  // removing a document prunes its cache
  os.Remove(filepath.Join(l.Root, "go", "Go Memory Model.md"))
  l.Build(ctx, nil)
  gone, _ := os.ReadDir(l.dir("cache"))
  if len(gone) != len(before) {
    t.Fatalf("cache files after removal = %d, want %d",
      len(gone), len(before))
  }
}

// catalogAnswer is a fake model's reply per book.
func catalogAnswer(t *testing.T, user string) string {
  switch {
  case strings.Contains(user, "Path: go/Effective Go.md"):
    if !strings.Contains(user, "line 4: Concurrency") {
      t.Fatalf("contents lack the headings:\n%s", user)
    }

    return "Here you go:\ntopic | where | note\n" +
      "Concurrency | § Concurrency (line 4) | goroutines and channels\n" +
      "go | whole book | idiomatic Go\n"

  case strings.Contains(user, "Path: os/OSTEP"):
    return "operating systems | threads-intro.md, vm-intro.md | " +
      "threads, address spaces\n"
  }

  return "not a table"
}

func TestCatalogAndTopics(t *testing.T) {
  l := testLibrary(t)
  ctx := context.Background()
  err := l.Build(ctx, nil)
  if err != nil {
    t.Fatal(err)
  }

  pending := l.Pending(nil)
  if len(pending) != 4 {
    t.Fatalf("Pending = %v, want 4 books", pending)
  }

  asked := 0
  ask := func(_ context.Context, system, user string) (string, error) {
    asked++
    if !strings.Contains(system, "topic | where | note") {
      t.Fatalf("system prompt lacks the format")
    }

    return catalogAnswer(t, user), nil
  }

  added, err := l.Catalog(ctx, ask, nil)
  if err != nil {
    t.Fatalf("Catalog: %v", err)
  }

  if added != 3 || asked != 4 {
    t.Fatalf("Catalog added %d rows over %d asks, want 3 over 4",
      added, asked)
  }

  rows := l.TopicRows("concurrency")
  if len(rows) != 1 || rows[0].Book != "go/Effective Go.md" ||
    rows[0].Note != "goroutines and channels" {
    t.Fatalf("TopicRows(concurrency) = %+v", rows)
  }

  all := l.TopicRows("")
  if len(all) != 3 || all[0].Topic != "concurrency" ||
    all[2].Topic != "operating-systems" {
    t.Fatalf("TopicRows() = %+v", all)
  }

  // books with no usable answer stay pending; answered ones don't
  pending = l.Pending(nil)
  if len(pending) != 2 {
    t.Fatalf("Pending after catalog = %v, want the 2 unanswered", pending)
  }

  md, _ := os.ReadFile(filepath.Join(l.Root, "TOPICS.md"))
  if !strings.Contains(string(md), "## concurrency") ||
    !strings.Contains(string(md), "**Effective Go**") {
    t.Fatalf("TOPICS.md:\n%s", md)
  }

  p := l.PromptIndex()
  if !strings.Contains(p, "- concurrency: Effective Go") ||
    !strings.Contains(p, "library_search") {
    t.Fatalf("PromptIndex:\n%s", p)
  }

  // the topic filter keeps to the cataloged books
  res, err := l.Search("address space", 5, "operating")
  if err != nil || len(res) == 0 || !strings.HasPrefix(res[0].Title, "OSTEP") {
    t.Fatalf("Search with topic = %+v, %v", res, err)
  }

  res, _ = l.Search("address space", 5, "concurrency")
  if len(res) != 0 {
    t.Fatalf("Search(concurrency) found %+v outside its books", res)
  }

  // a changed book is pending again
  time.Sleep(10 * time.Millisecond)
  writeDoc(t, l.Root, "os/OSTEP/locks.md",
    "# Locks\nA lock provides mutual exclusion.\n")
  found := false
  for _, b := range l.Pending(nil) {
    found = found || b == "os/OSTEP"
  }

  if !found {
    t.Fatal("a book with a new file isn't pending")
  }
}

func TestAdd(t *testing.T) {
  l := testLibrary(t)
  src := filepath.Join(t.TempDir(), "notes.md")
  os.WriteFile(src, []byte("# Notes\nSomething new about epoll.\n"), 0o644)

  added, err := l.Add([]string{ src }, "linux")
  if err != nil || len(added) != 1 || added[0] != "linux/notes.md" {
    t.Fatalf("Add = %v, %v", added, err)
  }

  _, err = l.Add([]string{ src }, "../outside")
  if err == nil {
    t.Fatal("Add outside the library succeeded")
  }

  l.Build(context.Background(), nil)
  res, _ := l.Search("epoll", 3, "")
  if len(res) == 0 || res[0].Title != "notes" {
    t.Fatalf("added document not found: %+v", res)
  }
}

func TestExtract(t *testing.T) {
  ps := extractHTML("<h1>A</h1><p>one &lt;two&gt;</p>" +
    "<!-- secret --><h3 class=x>B</h3>three")
  if len(ps) != 2 || ps[0].Label != "A" || ps[0].Text != "A. one <two>" ||
    ps[1].Label != "B" {
    t.Fatalf("extractHTML = %+v", ps)
  }

  ps = extractLines("Title\n=====\nbody\n\n## Sub\nmore\n", kindMarkdown)
  if len(ps) != 2 || ps[0].Label != "Title" || ps[1].Label != "Sub" ||
    ps[1].Loc != 5 {
    t.Fatalf("extractLines = %+v", ps)
  }

  long := strings.Repeat("line of text\n", 150)
  ps = extractLines(long, kindText)
  if len(ps) != 3 || ps[1].Loc != 61 {
    t.Fatalf("windows = %d pieces, second at %d", len(ps), ps[1].Loc)
  }

  got := squeeze("exam-\nple   text\n\n")
  if got != "example text" {
    t.Fatalf("squeeze = %q", got)
  }
}

func TestTerms(t *testing.T) {
  got := queryTerms("The sync.WaitGroup waits for Goroutines; " +
    "1234567 x ÉCOLE threads")
  want := "sync waitgroup wait goroutine école thread"
  if strings.Join(got, " ") != want {
    t.Fatalf("queryTerms = %q, want %q", strings.Join(got, " "), want)
  }

  buf := make([]byte, 0, 64)
  text := "Share memory by communicating; don't communicate by " +
    "sharing memory."
  allocs := testing.AllocsPerRun(100, func() {
    eachTerm(text, buf, func([]byte) {})
  })

  if allocs != 0 {
    t.Fatalf("eachTerm allocates %.0f times per call", allocs)
  }
}

func TestSnippet(t *testing.T) {
  text := strings.Repeat("filler words here ", 100) +
    "the needle is here " + strings.Repeat("more filler ", 100)
  s := snippet(text, []string{ "needle" })
  if !strings.Contains(s, "needle") || len(s) > snippetLen + 10 ||
    !strings.HasPrefix(s, "…") {
    t.Fatalf("snippet = %q", s)
  }
}

func TestMergeTopics(t *testing.T) {
  l := testLibrary(t)
  ctx := context.Background()
  err := l.Build(ctx, nil)
  if err != nil {
    t.Fatal(err)
  }

  l.saveTopics([]Topic{
    { Topic: "concurrency", Book: "go/Effective Go.md", Where: "§ 2" },
    { Topic: "concurrency-go", Book: "go/Effective Go.md", Where: "§ 2" },
    { Topic: "goroutines", Book: "go/Effective Go.md", Where: "§ 3" },
    { Topic: "threads", Book: "os/OSTEP", Where: "threads-intro.md" },
  })

  ask := func(_ context.Context, system, user string) (string, error) {
    if !strings.Contains(user, "concurrency-go (1)") ||
      !strings.Contains(system, "about 60") {
      t.Fatalf("merge prompt:\n%s\n%s", system, user)
    }

    return "Sure:\nconcurrency-go -> concurrency\n" +
      "- goroutines -> Concurrency\nthreads -> processes threads\n" +
      "made-up -> nothing\n", nil
  }

  before, after, err := l.MergeTopics(ctx, ask)
  if err != nil || before != 4 || after != 2 {
    t.Fatalf("MergeTopics = %d → %d, %v; want 4 → 2", before, after, err)
  }

  // the duplicate Effective Go row is gone, the others renamed
  rows := l.TopicRows("concurrency")
  if len(rows) != 2 {
    t.Fatalf("concurrency rows = %+v, want 2", rows)
  }

  if len(l.TopicRows("processes-threads")) != 1 {
    t.Fatalf("threads wasn't renamed: %+v", l.TopicRows(""))
  }

  // later catalogs use the canonical names
  aliases := l.aliases()
  if aliases["goroutines"] != "concurrency" || aliases["made-up"] != "" {
    t.Fatalf("aliases = %v", aliases)
  }

  ask2 := func(_ context.Context, _, user string) (string, error) {
    if strings.Contains(user, "Path: go/Effective Go.md") {
      return "goroutines | § Concurrency | channels\n", nil
    }

    return "", nil
  }

  l.Catalog(ctx, ask2, nil)
  for _, r := range l.TopicRows("goroutines") {
    t.Fatalf("a catalog wrote the merged name back: %+v", r)
  }
}

// manyTopics is n distinct topics, one row each.
func manyTopics(n int) []Topic {
  rows := make([]Topic, n)
  for i := range rows {
    rows[i] = Topic{ Topic: fmt.Sprintf("topic-%03d", i), Book: "b", Where: "§ 1" }
  }

  return rows
}

func TestMergeRoundsAndMemo(t *testing.T) {
  l := testLibrary(t)
  ctx := context.Background()
  err := l.Build(ctx, nil)
  if err != nil {
    t.Fatal(err)
  }

  // Each round folds pairs: 120 → 60 needs one round.
  calls := 0
  halve := func(_ context.Context, _, user string) (string, error) {
    calls++
    var b strings.Builder
    for i := 0; i < 200; i++ {
      fmt.Fprintf(&b, "topic-%03d -> topic-%03d\n", i, i / 2 * 2)
    }

    return b.String(), nil
  }

  l.saveTopics(manyTopics(120))
  before, after, err := l.MergeTopics(ctx, halve)
  if err != nil || before != 120 || after != 60 || calls != 1 {
    t.Fatalf("MergeTopics = %d → %d, %v in %d calls", before, after, err, calls)
  }

  // A model that merges nothing: one round, and no merge asked for
  // again until the topics change.
  l2 := testLibrary(t)
  err = l2.Build(ctx, nil)
  if err != nil {
    t.Fatal(err)
  }

  calls = 0
  none := func(context.Context, string, string) (string, error) {
    calls++
    return "", nil
  }

  l2.saveTopics(manyTopics(100))
  if !l2.NeedsMerge() {
    t.Fatal("100 topics don't need a merge")
  }

  _, after, err = l2.MergeTopics(ctx, none)
  if err != nil || after != 100 || calls != 1 {
    t.Fatalf("no-op merge: after %d, %v, %d calls", after, err, calls)
  }

  if l2.NeedsMerge() {
    t.Error("an unchanged table asks for the same merge again")
  }

  l2.saveTopics(manyTopics(101))
  if !l2.NeedsMerge() {
    t.Error("a new topic doesn't ask for a merge")
  }
}
