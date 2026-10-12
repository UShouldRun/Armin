package tools

type GitLogInput struct {
  Path  string `json:"path,omitempty"`
  Count int    `json:"count,omitempty"` // default 20, max 100
}

type GitCommit struct {
  Hash    string `json:"hash"`
  Author  string `json:"author"`
  Date    string `json:"date"`
  Subject string `json:"subject"`
}

type GitLogOutput struct {
  Commits []GitCommit `json:"commits"`
}

type GitBlameInput struct {
  Path      string `json:"path"`
  StartLine int    `json:"start_line,omitempty"` // 1-indexed; default 1
  EndLine   int    `json:"end_line,omitempty"`   // default start_line+199
}

type BlameLine struct {
  Line    int    `json:"line"`
  Author  string `json:"author"`
  Date    string `json:"date"`
  Summary string `json:"summary"`
}

type GitBlameOutput struct {
  Lines []BlameLine `json:"lines"`
}

type GitDiffInput struct {
  Path string `json:"path,omitempty"`
  From string `json:"from,omitempty"` // ref; default HEAD
  To   string `json:"to,omitempty"`   // ref; default the working tree
}

type GitDiffOutput struct {
  Diff      string `json:"diff"`
  Truncated bool   `json:"truncated,omitempty"`
}

type GitShowInput struct {
  Hash string `json:"hash"`
}

type GitShowOutput struct {
  Show      string `json:"show"`
  Truncated bool   `json:"truncated,omitempty"`
}

type WorkingTreeDiffInput struct {
  Path string `json:"path,omitempty"`
}

type WorkingTreeDiffOutput struct {
  Diff      string   `json:"diff"`
  Untracked []string `json:"untracked,omitempty"`
  Truncated bool     `json:"truncated,omitempty"`
}

type GrepHistoryInput struct {
  Term  string `json:"term"`
  Path  string `json:"path,omitempty"`
  Count int    `json:"count,omitempty"` // commits to return, default 10, max 50
}

type GrepHistoryOutput struct {
  Log       string `json:"log"`
  Truncated bool   `json:"truncated,omitempty"`
}

type CodeOwnersInput struct {
  Path string `json:"path,omitempty"`
}

type AuthorCount struct {
  Author  string `json:"author"`
  Commits int    `json:"commits"`
}

type CodeOwnersOutput struct {
  Owners []AuthorCount `json:"owners"` // ranked by commit count, most first
}

type CommitStatsInput struct {
  Path  string `json:"path,omitempty"`
  Count int    `json:"count,omitempty"` // top-N files to return, default 10, max 50
}

type FileChangeCount struct {
  File    string `json:"file"`
  Commits int    `json:"commits"`
}

type CommitStatsOutput struct {
  Hotspots []FileChangeCount `json:"hotspots"` // most-frequently-changed files, ranked
}
