package notes

import (
  "time"
)

// Scratchpad is a per-project directory of working notes: plans,
// findings, intermediate reasoning. The model writes there instead
// of carrying everything in context, and keeps only "what's where".
type Scratchpad struct {
  Dir string
}

// ScratchFile is one entry of List.
type ScratchFile struct {
  Name     string    `json:"name"`
  Bytes    int64     `json:"bytes"`
  Lines    int       `json:"lines"`
  Modified time.Time `json:"modified"`
}
