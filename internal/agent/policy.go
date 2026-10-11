package agent

import "context"

// WindowControl is a model with two context tiers: a resident one
// (whole KV cache in VRAM, fastest) and a larger streaming one (KV
// cache in RAM, streamed through a ring buffer). llamacpp.Window
// implements it.
type WindowControl interface {
  Size() int     // the active tier's window
  Resident() int // largest window that stays in VRAM
  Max() int      // largest window available
  Streaming() bool
  Grow(ctx context.Context) (int, error)
  Shrink(ctx context.Context) (int, error)
}

// Action is one context-management step; a decision packs several.
type Action uint8

const (
  ActTrim    Action = 1 << iota // stub old tool results (archived to the scratchpad)
  ActSave                       // let the model save notes before compacting
  ActCompact                    // summarize older history
  ActGrow                       // move to the streaming window
  ActShrink                     // move back to the resident window
  ActAll     = ActTrim | ActSave | ActCompact | ActGrow | ActShrink
)

// ContextState is what Decide looks at. Token counts are estimates.
type ContextState struct {
  Used, Window, Resident, Max int
  // Turn is the tokens since the current user message: the working
  // set the model is using right now.
  Turn int
  // Fixed is what every request carries regardless of history (the
  // system prompt and tool definitions): no compaction removes it.
  Fixed     int
  Streaming bool
}

// Thresholds, as percent of the active window. Below trimAt nothing
// changes, so the prompt prefix stays byte-stable and llama-server's
// KV cache keeps hitting; past it, old tool results go (in a batch);
// past DefaultCompactAt the next request is at risk, and something
// bigger has to happen. shrinkAt is of the resident window.
const (
  trimAt           = 50
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

// DecideAfterCompact checks whether a compaction left so little that
// the streaming tier can go. Its re-prefill is nearly free here:
// compaction rewrote history, so the prefix cache is gone anyway.
func DecideAfterCompact(s ContextState) Action {
  if s.Streaming && s.Used < s.Resident * shrinkAt / 100 {
    return ActShrink
  }

  return 0
}
