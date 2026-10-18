// What Parse produces: the Options every later step reads.

package input

import (
  "time"

  "armin/internal/agent"
  "armin/internal/config"
  "armin/internal/session"
)

// Mode is which frontend shows armin (see package display).
type Mode uint8

// Flag is one on/off option.
type Flag uint8

// Options is what the user asked for, flags over config.conf.
type Options struct {
  Root, Model, CompletionModel     string
  LlamaURL, CompletionLlamaURL     string
  CtagsBin, WebAddr                string
  AddDirs                          []string
  LibraryAdd                       []string // files to copy in
  LibraryTo                        string   // their topic folder
  Effort                           agent.Effort
  Aids                             agent.Aids
  Timeout                          time.Duration
  MaxOutput, MaxRounds             int
  Mode                             Mode
  Flags                            Flag

  Config  config.Config  // config.conf, for what has no flag
  Resumed *session.Saved // the session to continue, if any
}
