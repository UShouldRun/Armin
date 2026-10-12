package tools

// ReadPDFInput is the argument set for read_pdf.
type ReadPDFInput struct {
  Path      string `json:"path"`
  FirstPage int    `json:"first_page,omitempty"` // 1-indexed, default 1
  LastPage  int    `json:"last_page,omitempty"`  // default first + 9
}

type ReadPDFOutput struct {
  Text       string `json:"text"`
  FirstPage  int    `json:"first_page"`
  LastPage   int    `json:"last_page"`
  TotalPages int    `json:"total_pages"`
  Truncated  bool   `json:"truncated,omitempty"`
  Next       string `json:"next,omitempty"` // how to continue
}
