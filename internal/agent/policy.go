// The context policy as pure functions of a ContextState: how full
// the window is and what to do about it, kept apart from the loop so
// it's easy to read and test.

package agent

const (
  ActTrim    Action = 1 << iota // stub old tool results (archived first)
  ActSave                       // let the model save notes before compacting
  ActCompact                    // summarize older history
  ActGrow                       // move to the streaming window
  ActShrink                     // move back to the resident window
  ActAll     = ActTrim | ActSave | ActCompact | ActGrow | ActShrink
)

// Thresholds, as percent of the active window. Below trimAt nothing
// changes, so the prompt prefix stays byte-stable and llama-server's
// KV cache keeps hitting; past it, old tool results go (in a batch);
// past suggestAt the model offers the user a compaction; past
// DefaultCompactAt the next request is at risk, and something
// bigger has to happen. shrinkAt is of the resident window.
const (
  trimAt           = 50
  suggestAt        = 60
  DefaultCompactAt = 75
  shrinkAt         = 50
)

// Decide picks the cheapest steps that keep the next request inside
// the window, before any trimming has happened.
//
// The judgment call is past DefaultCompactAt: compacting summarizes
// older history, which is cheap and keeps decoding fast, but loses
// detail; growing keeps everything, but costs a restart and a full
// re-prefill, and decodes slower once the KV cache spills into RAM.
// So: if most of the history is the current turn — files the model
// is in the middle of working with — summarizing would throw away
// exactly what it needs, and the window grows. If most of it is
// older history, that history is what summaries are for.
func Decide(s ContextState) Action {
  if s.Window <= 0 {
    return 0
  }

  if s.Used <= s.Window * trimAt / 100 {
    return 0
  }

  act := ActTrim
  if s.Used <= s.Window * DefaultCompactAt / 100 {
    return act
  }

  canGrow := !s.Streaming && s.Max > s.Window
  workingSet := s.Turn * 2 > s.Used - s.Fixed
  if canGrow && workingSet {
    return act | ActGrow
  }

  return act | ActSave | ActCompact
}

// SuggestCompact reports whether the model should offer the user a
// compaction: past suggestAt a few more turns reach DefaultCompactAt,
// and a compaction the user picks at a break between tasks loses
// less than the automatic one landing in the middle of one.
func SuggestCompact(s ContextState) bool {
  if s.Window <= 0 {
    return false
  }

  return s.Used > s.Window * suggestAt / 100 &&
    s.Used <= s.Window * DefaultCompactAt / 100
}

// DecideAfterCompact checks whether a compaction left so little that
// the streaming tier can go. Its re-prefill is nearly free here:
// compaction rewrote history, so the prefix cache is gone anyway.
func DecideAfterCompact(s ContextState) Action {
  if s.Streaming && s.Used < s.Resident * shrinkAt / 100 {
    return ActShrink
  }

  return 0
}
