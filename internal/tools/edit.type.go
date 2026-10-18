// Inputs and outputs of edit.go's tools.

package tools

import (
  "os"
  "sync"
  "time"
)

// FileTracker remembers which files the model has read this session
// and their mtime at that point, so a write can't clobber a file
// the model never looked at, and an edit can't apply on top of a
// file that changed under it. Safe for concurrent use.
type FileTracker struct {
  mu   sync.Mutex
  seen map[string]time.Time
}

// Change is a planned write: computed, previewable, and only
// touching disk on Apply. Splitting the two lets the REPL show a
// diff and ask before anything is written.
type Change struct {
  Path, Resolved string
  Old, New       string
  Replacements   int
  mode           os.FileMode
  Created        bool
}

// ChangeOutput is what the model sees after a write.
type ChangeOutput struct {
  Path         string `json:"path"`
  Replacements int    `json:"replacements,omitempty"`
  LinesAdded   int    `json:"lines_added"`
  LinesRemoved int    `json:"lines_removed"`
  Created      bool   `json:"created,omitempty"`
  DryRun       bool   `json:"dry_run,omitempty"`
}

type WriteFileInput struct {
  Path    string `json:"path"`
  Content string `json:"content"`
}

type EditFileInput struct {
  Path       string `json:"path"`
  OldString  string `json:"old_string"`
  NewString  string `json:"new_string"`
  ReplaceAll bool   `json:"replace_all,omitempty"`
}
