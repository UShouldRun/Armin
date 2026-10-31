# The reference library

## Reference library

`library = <folder>` in config.conf gives the model a shelf of real
references to look facts up in instead of recalling them: books,
official docs, language standards, RFCs, man pages. Top-level
folders are topics (`go`, `c`, `linux`, `math`, ...); each file or
folder inside one is a book. PDFs are read page by page (poppler's
`pdftotext`), HTML, Markdown, reStructuredText, man pages and plain
text section by section.

- **`library_search`** ranks passages with BM25 over an inverted
  index in `<folder>/.armin/index` (only a small term table is kept
  in memory; postings and text are read from disk per query). Each
  result has the title, file, page or line, the passage, and the
  `read_pdf` / `read_file` call that reads on — the folder is
  readable without asking. `topic` keeps a search to one topic's
  books.
- **`library_topics`** answers "where is X covered": book, chapters
  or pages, and what it's good for.
- The **system prompt** carries a short "where to look" line per
  topic and asks the model to cite `[Title, page N]` with a Library
  sources list of files, so you can check every claim.

The topic table (`<folder>/.armin/topics.tsv`, and `TOPICS.md` for
reading) is written by a model: for each new or changed book it reads
the table of contents (or a folder's files and headings) and answers
`topic | where | note` lines. That runs on `armin -library-sync`
(install.sh offers it), `/library catalog [model]`, and after adding
files with `/library add <path>... [to <folder>]` or `armin
-library-add a.pdf,notes/ -library-to go`. A hosted model makes quick
work of it (`/library catalog planner`); hand edits to the table stay
until that book changes and is cataloged again. armin reindexes in
the background on startup when files changed, and says when books are
waiting to be cataloged.

Models name topics freely, so past 80 distinct topics a merge pass
(also `/library merge [model]`) has the model fold synonyms and
narrow topics into about 60 canonical ones; the mapping is kept in
`.armin/aliases.tsv`, and later catalogs use the canonical names.

[docs/library-fetch.sh](../library-fetch.sh) downloads a starter
library of freely licensed books and docs (Go and C references,
Beej's guides, OS, networking, databases, security, performance,
math, physics) into `~/Documents/Software`.

