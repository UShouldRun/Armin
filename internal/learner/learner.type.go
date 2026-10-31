// The types of learner.go, status.go and probe.go.

package learner

import (
  "os"

  "armin/internal/know"
)

// EventKind is what produced an event, in 2 bits.
type EventKind uint8

const (
  EventQuiz        EventKind = iota // a check question
  EventSelfReport                   // what the user says they know
  EventExplanation                  // the user's own explanation, read
  EventRefresher                    // a refresher's check question
)

// Result is an event's outcome, in 2 bits. For a self-report it is the
// level instead: ReportNever, ReportHeard, ReportUsed, ReportExplain.
type Result uint8

// For an explanation, Right is a concept used correctly, Wrong a
// misconception, Partial a gap: needed, never touched.
const (
  Wrong   Result = iota
  Partial
  Right

  ReportNever   Result = 0
  ReportHeard   Result = 1
  ReportUsed    Result = 2
  ReportExplain Result = 3
)

// Sure is how sure the user said they were, in 2 bits; a self-report
// of ReportExplain with SureCertain declares a foundation (crystallized).
type Sure uint8

const (
  SureNot Sure = iota
  SureSomewhat
  SureMostly
  SureCertain
)

// Event is one record of the events file: 12 bytes, append-only.
type Event struct {
  When uint32 // unix minutes
  Node know.NodeID
  Bits uint8 // kind in bits 0-1, result in 2-3, sure in 4-5
  _    [3]uint8
}

// Status is what is known about one concept, packed in 4 bytes and
// rebuilt from the events at load:
//
//   bits 0-2    Leitner box 0-7: trusted for about 2^box days
//   bit  3      crystallized: never fades (counting, English)
//   bits 4-19   the day of the last answer (days since 2026-01-01)
//   bits 20-25  accuracy score 0-50 of a crystallized concept
//   bit  26     known by implication (a dependent was answered right)
//   bit  27     has any status at all
//   bit  28     in doubt: a partial answer, or an explanation that
//               needed it and never touched it; cleared by a right one
type Status uint32

// State is what a plan does with a concept.
type State uint8

const (
  Unknown  State = iota // teach it
  NotSure               // ask a quick check question first
  Implied               // known by implication: confirm if a plan rests on it
  Known                 // skip it
)

// Learner is the learner tree (a know graph of topics and concepts)
// with the events about the user and the status they add up to.
type Learner struct {
  Tree   *know.Graph
  events []Event
  status []Status
  file   *os.File
}

// Probe finds what the user knows of a set of concepts with few
// questions: each answer settles a concept's prerequisites (right)
// or its dependents (wrong), and the next question goes where it
// settles the most either way.
type Probe struct {
  nodes   []know.NodeID
  index   map[know.NodeID]int
  below   [][]uint64 // bitset rows: each node's prerequisites, at any depth
  above   [][]uint64 // and its dependents
  settled []uint64
  known   []uint64
}
