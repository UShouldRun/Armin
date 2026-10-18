// The types of library.go.

package library

import (
  "context"
  "os"
  "sync"
)

// Kind is how a document's text is laid out.
type Kind uint8

// Doc is one file in the library.
type Doc struct {
  Path  string // relative to the root, slash-separated
  Book  string // the unit it belongs to: a topic dir's child
  Size  int64
  Mtime int64
  Kind  Kind
}

// piece is a passage before indexing: a PDF page or a section.
type piece struct {
  Label string // the section heading, if any
  Loc   uint32 // page (PDF) or first line (text)
  Text  string
}

// cached is one document's pieces in .armin/cache.
type cached struct {
  Size, Mtime int64
  Pieces      []piece
}

// chunk is an indexed piece; its text lives in text.bin.
type chunk struct {
  Doc, Loc   uint32
  Off        uint64
  Len, Words uint32
}

// sparseTerm marks every sparseEvery-th record of terms.bin, so a
// lookup reads one small block instead of the whole dictionary.
type sparseTerm struct {
  Term string
  Off  int64
}

// meta is index/meta.gob.
type meta struct {
  Version int
  Docs    []Doc
  AvgLen  float64
  Sparse  []sparseTerm
  TermsAt int64 // size of terms.bin
}

// postings is one term's chunks, in increasing order, with the
// term's count in each.
type postings struct {
  chunks []uint32
  tfs    []uint16
}

// indexFile is one file of an index directory and where it opens.
type indexFile struct {
  f    **os.File
  name string
}

// index is an opened index directory.
type index struct {
  meta              meta
  chunks            []chunk
  terms, post, text *os.File
}

// Topic is one row of the topic table: where a topic is covered.
type Topic struct {
  Topic string `json:"topic"`
  Book  string `json:"book,omitempty"`
  Where string `json:"where"`
  Note  string `json:"note,omitempty"`
}

// Result is one passage library_search returns.
type Result struct {
  ID       int    `json:"id"`
  Title    string `json:"title"`
  File     string `json:"file"`
  Where    string `json:"where"`
  Section  string `json:"section,omitempty"`
  Text     string `json:"text"`
  ReadMore string `json:"read_more"`
}

// Status is what /library shows.
type Status struct {
  Root                  string
  Docs, Books, Chunks   int
  Topics, Pending       int
  Stale                 int // new, changed or removed documents
  Building              string // progress while (re)indexing
}

// Ask sends one system + user message to a model and returns its
// reply; the library doesn't depend on any client.
type Ask func(ctx context.Context, system, user string) (string, error)

// Progress hears how far a long job is.
type Progress func(done, total int, what string)

// Library is a folder of reference documents with a search index
// and a topic table.
type Library struct {
  Root string

  mu       sync.Mutex
  idx      *index
  building string // non-empty while Build runs
  topics   []Topic
  topicsAt int64 // topics.tsv mtime when loaded
}
