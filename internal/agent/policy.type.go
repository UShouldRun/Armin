// The context policy's inputs and outcomes.

package agent

import (
  "context"
)

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
