package think

import (
  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/library"
  "armin/internal/llamacpp"
  "armin/internal/llm"
  "armin/internal/notes"
  "armin/internal/sandbox"
  "armin/internal/searxng"
)

// Mind is everything a frontend needs to run turns.
type Mind struct {
  Sandbox   *sandbox.Config
  WebSearch *searxng.Manager // nil without docker
  Symbols   *ctags.Index
  Library   *library.Library // nil without library = in config.conf

  // Nil in input.ModeMCP, which serves the tools alone.
  Models   *llamacpp.Manager
  Client   *llm.Client

  // Completion serves inline completion. Without its own
  // completion_model it follows Client between local models, and
  // stays local when chat moves to a cloud one (see Follower).
  Completion *llm.Client
  follows    bool

  Loop     *agent.Loop
  Memories *notes.Memories
  Skills   *notes.Skills
  Scratch  *notes.Scratchpad
}
