// Inputs and outputs of repostat.go's tools.

package tools

// dirCount is one directory or extension's aggregate — files and
// lines seen under it.
type dirCount struct{ files, lines int }

// repoStats is one full-tree walk's result, shared by CountLines and
// ListLanguages so two tools that are often asked together don't
// each pay for their own walk of a potentially large repo.
type repoStats struct {
  totalFiles, totalLines int
  byTopDir               map[string]dirCount
  byExt                  map[string]dirCount
}

type CountLinesInput struct {
  Path string `json:"path,omitempty"` // default: project root
}

type DirLineCount struct {
  Dir   string `json:"dir"`
  Files int    `json:"files"`
  Lines int    `json:"lines"`
}

type CountLinesOutput struct {
  TotalFiles int            `json:"total_files"`
  TotalLines int            `json:"total_lines"`
  ByTopDir   []DirLineCount `json:"by_top_dir,omitempty"`
}

type ListLanguagesInput struct {
  Path string `json:"path,omitempty"`
}

type LanguageCount struct {
  Extension string `json:"extension"`
  Files     int    `json:"files"`
  Lines     int    `json:"lines"`
}

type ListLanguagesOutput struct {
  Languages []LanguageCount `json:"languages"` // ranked by lines, most first
}

type FindFilesInput struct {
  Pattern string `json:"pattern"` // filepath.Match glob against the base name, e.g. "*.go"
  Path    string `json:"path,omitempty"`
}

type FindFilesOutput struct {
  Files     []string `json:"files"`
  Truncated bool     `json:"truncated,omitempty"`
}

type FileStatInput struct {
  Path string `json:"path"`
}

type FileStatOutput struct {
  SizeBytes int64  `json:"size_bytes"`
  Lines     int    `json:"lines,omitempty"` // 0 for binary files
  ModTime   string `json:"mod_time"`        // RFC3339
  IsBinary  bool   `json:"is_binary,omitempty"`
}

type FindTodoFixmeInput struct {
  Path string `json:"path,omitempty"`
}

type FindTodoFixmeOutput struct {
  Matches   string `json:"matches"`
  Truncated bool   `json:"truncated,omitempty"`
}

type EnvInfoInput struct{}

type EnvInfoOutput struct {
  GoVersion string `json:"go_version,omitempty"`
  OS        string `json:"os"`
  Arch      string `json:"arch"`
  HasGit    bool   `json:"has_git"`
  HasRg     bool   `json:"has_ripgrep"`
  HasDocker bool   `json:"has_docker"`
}

type ProjectManifestInput struct{}

type ProjectManifestOutput struct {
  Note string `json:"note"` // directory tree + the first manifest/README found
}
