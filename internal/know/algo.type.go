// The types of algo.go.

package know

// TraceStep is one node a conclusion rests on.
type TraceStep struct {
  ID    NodeID
  Depth int
}

// CycleError is Plan's refusal: the prerequisites of the goal loop.
type CycleError struct {
  Nodes []NodeID
}

// readyHeap orders the nodes ready to teach: most unlocked first,
// then fewest prerequisites, then lowest ID.
type readyHeap struct {
  items   []int
  unlocks []int
  prereq  [][]int
  nodes   []NodeID
}
