package agent

type source struct {
  title, url string
}

// turnSources numbers web results across one turn, so two searches
// never hand out the same [n]. Turn owns one; nil disables numbering
// (the save-before-compact exchange, MCP).
type turnSources struct {
  list  []source
  byURL map[string]int
}
