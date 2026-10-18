package repl

import (
  "context"
  "fmt"
  "strings"

  "armin/internal/library"
  "armin/internal/llm"
  "armin/internal/ui"
)

// cmdLibrary is /library: the reference library's status, and the
// ways to change it.
//
//   /library                         status
//   /library index                   reindex now
//   /library catalog [model]         topic rows for new books
//   /library add <path>... [to dir]  copy files in, index, catalog
//   /library merge [model]           fold similar topics together
//   /library topics [topic]          the topic table
func cmdLibrary(r *REPL, ctx context.Context, args []string) {
  lib := r.Loop.Library
  if lib == nil {
    ui.Error("no library configured: add library = <folder> to " +
      "config.conf and restart armin")
    return
  }

  if len(args) == 0 {
    printLibrary(lib)
    return
  }

  switch args[0] {
  case "index":
    err := lib.Build(ctx, func(done, total int, what string) {
      line := fmt.Sprintf("indexing %d/%d %s", done, total, what)
      fmt.Printf("\r\033[K%s", ui.Dim(line))
    })
    fmt.Println()
    if err != nil {
      ui.Error("%s", err)
      return
    }

    printLibrary(lib)

  case "catalog":
    client := r.LLM
    if len(args) > 1 {
      c, err := r.catalogClient(ctx, args[1])
      if err != nil {
        ui.Error("%s", err)
        return
      }

      client = c
    }

    err := lib.Sync(ctx, library.AskWith(client), client.Model)
    if err != nil {
      ui.Error("%s", err)
    }

  case "merge":
    client := r.LLM
    if len(args) > 1 {
      c, err := r.catalogClient(ctx, args[1])
      if err != nil {
        ui.Error("%s", err)
        return
      }

      client = c
    }

    before, after, err := lib.MergeTopics(ctx, library.AskWith(client))
    if err != nil {
      ui.Error("%s", err)
      return
    }

    fmt.Printf("%d topics merged into %d\n", before, after)

  case "add":
    paths, dir := args[1:], ""
    if len(paths) >= 2 && paths[len(paths) - 2] == "to" {
      dir = paths[len(paths) - 1]
      paths = paths[:len(paths) - 2]
    }

    if len(paths) == 0 {
      ui.Error("usage: /library add <path>... [to <folder>]")
      return
    }

    added, err := lib.Add(paths, dir)
    for _, a := range added {
      fmt.Println("added " + a)
    }

    if err != nil {
      ui.Error("%s", err)
      return
    }

    err = lib.Sync(ctx, library.AskWith(r.LLM), r.LLM.Model)
    if err != nil {
      ui.Error("%s", err)
    }

  case "topics":
    topic := strings.Join(args[1:], " ")
    rows := lib.TopicRows(topic)
    if len(rows) == 0 {
      fmt.Println(ui.Dim("no topics yet: /library catalog"))
      return
    }

    for _, t := range rows {
      if t.Book == "" {
        fmt.Printf("  %-28s %s\n", t.Topic, ui.Dim(t.Where))
        continue
      }

      title := ui.Bold(library.BookTitle(t.Book))
      fmt.Printf("  %-22s %s: %s %s\n",
        t.Topic, title, t.Where, ui.Dim(t.Note))
    }

  default:
    ui.Error("usage: /library [index|catalog [model]|merge [model]|" +
      "add <path>... [to <folder>]|topics [topic]]")
  }
}

// catalogClient is a client for cataloging with another model than
// chat's: a hosted one, or the local one already loaded (two local
// models rarely fit one GPU).
func (r *REPL) catalogClient(
  ctx context.Context, name string,
) (*llm.Client, error) {
  if !r.Models.Has(name) {
    return nil, fmt.Errorf("unknown model %q (see /model)", name)
  }

  if r.Models.IsLocal(name) && name != r.LLM.Model {
    return nil, fmt.Errorf(
      "%q is local: switch to it with /model first, or catalog "+
        "with a hosted model",
      name,
    )
  }

  return r.Models.Client(ctx, name)
}

func printLibrary(lib *library.Library) {
  s := lib.Status()
  fmt.Println(ui.Bold("library"))
  fmt.Printf("  folder:    %s\n", s.Root)
  fmt.Printf("  documents: %d in %d books, %d passages indexed\n",
    s.Docs, s.Books, s.Chunks)
  fmt.Printf("  topics:    %d rows (%s/TOPICS.md)\n", s.Topics, s.Root)

  switch {
  case s.Building != "":
    fmt.Printf("  indexing:  %s\n", s.Building)
  case s.Stale > 0:
    fmt.Printf("  stale:     %d documents changed since indexing"+
      " — /library index\n", s.Stale)
  }

  if s.Pending > 0 {
    fmt.Printf("  pending:   %d books not in the topic table"+
      " — /library catalog\n", s.Pending)
  }
}
