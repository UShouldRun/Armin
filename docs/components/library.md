# The library

A shelf of real references the model looks facts up in instead of
recalling them: books, official docs, standards, RFCs, man pages.
Package `internal/library`; set with `library = <folder>` in
config.conf. Top-level folders are topics (`go`, `c`, `math`, ...);
each file or folder inside one is a book.

## Searching

- **Extraction** (`extract.go`): PDFs page by page (`pdftotext`);
  HTML, Markdown, reStructuredText, man pages and text section by
  section, long sections in windows. Cached per file in
  `<folder>/.armin/cache`, so only new or changed files are read
  again.
- **Index** (`index.go`): BM25 over every passage, stored on disk in
  `.armin/index`. Only a small table of every 64th term stays in
  memory; a query reads one block of the dictionary and its postings
  from disk. Rebuilt in the background when files change.
- **Search** (`search.go`): ranks passages, favors ones holding more
  of the query's words, at most two per document, and can keep to one
  topic's books. Each result has the title, file, page or line, the
  passage, and the `read_pdf` / `read_file` call that reads on.

## The topic table

A model writes it (`catalog.go`): for each new or changed book it
reads the table of contents (or a folder's files) and answers
`topic | where | note` lines, kept in `.armin/topics.tsv` and
`TOPICS.md`. Past 80 topics, a merge pass folds synonyms into about 60
canonical ones and remembers the mapping (`merge.go`). The system
prompt carries a "where to look" line per topic (`PromptIndex`).

It runs on `armin -library-sync` (install.sh offers it), `/library
catalog`, and after adding books with `/library add`.

## Files

| file | what's in it |
|---|---|
| `library.go` | `Open`, scanning, `Add`, `Status` |
| `extract.go` | text out of each kind of file; the tokenizer |
| `index.go` | building and opening the index |
| `search.go` | `Search` |
| `topics.go` | the topic table and the prompt's "where to look" |
| `catalog.go`, `merge.go`, `ask.go`, `sync.go` | the model's part: cataloging, merging, `Sync` |

## Connections

- opened by: `think.NewSenses` (its folder becomes readable)
- searched by: the agent's `library_search` and `library_topics`
- driven by: `/library` (repl), `-library-sync` (display)
