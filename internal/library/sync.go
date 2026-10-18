package library

import (
  "context"
  "fmt"

  "armin/internal/ui"
)

// Sync indexes the library and has ask's model catalog its pending
// books, printing progress on one line: -library-sync, /library
// catalog.
func (l *Library) Sync(ctx context.Context, ask Ask, model string) error {
  if l.Stale() {
    err := l.Build(ctx, progressLine("indexing"))
    fmt.Println()
    if err != nil {
      return fmt.Errorf("\n\t... in Sync:\n\t%w", err)
    }
  }

  s := l.Status()
  fmt.Printf(
    "%d documents in %d books, %d passages\n", s.Docs, s.Books, s.Chunks,
  )
  if s.Pending == 0 {
    fmt.Println("topic table up to date")
    return l.mergeIfNeeded(ctx, ask)
  }

  fmt.Printf("cataloging %d books with %s\n", s.Pending, model)
  rows, err := l.Catalog(ctx, ask, progressLine("cataloging"))
  fmt.Println()
  if err != nil {
    return fmt.Errorf("\n\t... in Sync:\n\t%w", err)
  }

  fmt.Printf(
    "%d topic rows added; the table is in %s/TOPICS.md\n", rows, l.Root,
  )

  return l.mergeIfNeeded(ctx, ask)
}

// mergeIfNeeded folds the topics together once there are too many.
func (l *Library) mergeIfNeeded(ctx context.Context, ask Ask) error {
  if !l.NeedsMerge() {
    return nil
  }

  fmt.Println("merging topics")
  before, after, err := l.MergeTopics(ctx, ask)
  if err != nil {
    return fmt.Errorf("\n\t... in mergeIfNeeded:\n\t%w", err)
  }

  fmt.Printf("%d topics merged into %d\n", before, after)

  return nil
}

// progressLine rewrites one terminal line with a job's progress.
func progressLine(job string) Progress {
  return func(done, total int, what string) {
    line := fmt.Sprintf("%s %d/%d %s", job, done, total, what)
    if len(line) > 100 {
      line = line[:97] + "…"
    }

    fmt.Printf("\r\033[K%s", ui.Dim(line))
  }
}
