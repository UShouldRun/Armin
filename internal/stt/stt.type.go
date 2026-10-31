// The types of stt.go.

package stt

import (
  "bytes"
  "os/exec"
)

// Recorder is a recording in progress.
type Recorder struct {
  cmd    *exec.Cmd
  path   string
  stderr *bytes.Buffer
}
