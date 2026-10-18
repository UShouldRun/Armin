package agent

// Aids are the reasoning aids a Loop applies; one bit each.
type Aids uint8

// Task is the kind of request auto's question saw, which picks the
// procedure the turn gets.
type Task uint8

// stall counts a turn's attempts that went nowhere, for the
// step-back note.
type stall struct {
  misses, nudges uint8
}
