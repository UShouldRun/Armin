// Mind: everything a frontend needs to run turns.

package think

import (
  "sync"

  "armin/internal/agent"
  "armin/internal/ctags"
  "armin/internal/know"
  "armin/internal/learner"
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
  Know     *know.Graph // the project's knowledge tree; nil if it can't open
  knowOnce sync.Once
  Learner  *learner.Learner // what the user knows; nil if it can't open
  Memories *notes.Memories
  Skills   *notes.Skills
  Scratch  *notes.Scratchpad
}
