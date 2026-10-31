// The rules that turn events into a concept's status.
//
// A concept sits in a Leitner box 0-7 and is trusted for about 2^box
// days after its last answer: right moves it up a box, wrong back to
// 0. Past that it has faded, and counts as "not sure" when a plan is
// built (a quick check question, no review schedule).
//
// Crystallized concepts never fade. One gets there declared (a self-
// report "could explain it", fully sure) or earned (box 7, answered
// right after the full 128 days). It keeps an accuracy score 0-50 that
// every answer moves toward 50 or 0 by an eighth, at least one step;
// below 30 it counts as "not sure" and a refresher is recommended.
//
// A right answer also says something about what the concept requires:
// prerequisites with no status become known by implication, and those
// with one have their last day refreshed (doing multiplication
// exercises addition).

package learner

import (
  "time"

  "armin/internal/know"
)

const (
  boxMask    Status = 0b111
  crystalBit Status = 1 << 3
  dayShift          = 4
  dayMask    Status = 0xffff << dayShift
  scoreShift        = 20
  scoreMask  Status = 0b111111 << scoreShift
  impliedBit Status = 1 << 26
  hasBit     Status = 1 << 27
  doubtBit   Status = 1 << 28

  // RefresherBelow is the score under which a crystallized concept
  // counts as not sure and a refresher is recommended.
  RefresherBelow = 30

  maxScore    = 50
  scoreWeight = 8 // an answer moves the score by 1/scoreWeight
  topBox      = 7
)

// epochDay is 2026-01-01 in days since the Unix epoch: the zero of the
// day numbers status keeps, as know's Born and Seen do.
const epochDay = 20454

// Day turns a time into a status day number.
func Day(t time.Time) uint16 {
  return uint16(t.Unix() / 86400 - epochDay)
}

func dayOfMinutes(m uint32) uint16 {
  return uint16(int64(m) / 1440 - epochDay)
}

func (s Status) box() int        { return int(s & boxMask) }
func (s Status) crystal() bool   { return s & crystalBit != 0 }
func (s Status) day() uint16     { return uint16((s & dayMask) >> dayShift) }
func (s Status) score() int      { return int((s & scoreMask) >> scoreShift) }
func (s Status) implied() bool   { return s & impliedBit != 0 }
func (s Status) has() bool       { return s & hasBit != 0 }

func (s Status) withBox(b int) Status      { return s &^ boxMask | Status(b) }
func (s Status) withDay(d uint16) Status   { return s &^ dayMask | Status(d) << dayShift }
func (s Status) withScore(v int) Status    { return s &^ scoreMask | Status(v) << scoreShift }

// moveScore moves s toward target by an eighth of the gap, rounded
// away from zero so an answer always counts: a plain average with
// floor would get stuck one step short.
func moveScore(s, target int) int {
  gap := target - s
  switch {
  case gap > 0:
    return s + (gap + scoreWeight - 1) / scoreWeight
  case gap < 0:
    return s - (-gap + scoreWeight - 1) / scoreWeight
  }

  return s
}

func (l *Learner) at(id know.NodeID) *Status {
  for int(id) >= len(l.status) {
    l.status = append(l.status, 0)
  }

  return &l.status[id]
}

// apply updates status for one event.
func (l *Learner) apply(e Event) {
  s := l.at(e.Node)
  old := *s
  day := dayOfMinutes(e.When)
  next := (old | hasBit) &^ impliedBit
  switch e.Kind() {
  case EventSelfReport:
    box := map[Result]int{ ReportNever: 0, ReportHeard: 0, ReportUsed: 3, ReportExplain: 5 }[e.Result()]
    if old.has() && !old.implied() {
      box = max(box, old.box())
    }

    next = next.withBox(box).withDay(day)
    if e.Result() == ReportExplain && e.Sure() == SureCertain && !next.crystal() {
      next = (next | crystalBit).withScore(maxScore)
    }

    *s = next
    if e.Result() == ReportExplain {
      l.prerequisites(e.Node, day)
    }

  default:
    switch e.Result() {
    case Right:
      next &^= doubtBit
      if next.crystal() {
        next = next.withScore(moveScore(next.score(), maxScore))
      } else {
        if old.has() && !old.implied() && old.box() == topBox &&
          day >= old.day() && int(day - old.day()) >= 1 << topBox {
          next = (next | crystalBit).withScore(l.historyScore(e.Node))
        }

        next = next.withBox(min(next.box() + 1, topBox))
      }

      *s = next.withDay(day)
      l.prerequisites(e.Node, day)

    case Partial:
      *s = (next | doubtBit).withDay(day)

    case Wrong:
      next &^= doubtBit // no doubt left: it isn't known
      if next.crystal() {
        next = next.withScore(moveScore(next.score(), 0))
      } else {
        next = next.withBox(0)
      }

      *s = next.withDay(day)
    }
  }
}

// historyScore is the share of right answers among a concept's
// answered events, as 0-50: the score a concept earns crystallization
// with.
func (l *Learner) historyScore(id know.NodeID) int {
  right, total := 1, 1 // the answer being applied, which is right
  for _, e := range l.events {
    if e.Node != id || e.Kind() == EventSelfReport {
      continue
    }

    total++
    if e.Result() == Right {
      right++
    }
  }

  return right * maxScore / total
}

// prerequisites marks what id requires, at any depth: no status yet
// becomes known by implication; a status gets its day refreshed.
func (l *Learner) prerequisites(id know.NodeID, day uint16) {
  seen := map[know.NodeID]bool{ id: true }
  queue := []know.NodeID{ id }
  var edges []know.Edge
  for len(queue) > 0 {
    n := queue[0]
    queue = queue[1:]
    edges = l.Tree.Out(n, edges[:0])
    for _, e := range edges {
      if e.Kind != know.EdgeRequires || seen[e.To] || !l.Tree.Live(e.To) {
        continue
      }

      seen[e.To] = true
      queue = append(queue, e.To)
      s := l.at(e.To)
      switch {
      case !s.has():
        *s = (hasBit | impliedBit).withDay(day)
      case day > s.day():
        *s = s.withDay(day)
      }
    }
  }
}

// State is what a plan should do with concept id today.
func (l *Learner) State(id know.NodeID, today uint16) State {
  if int(id) >= len(l.status) {
    return Unknown
  }

  s := l.status[id]
  switch {
  case !s.has():
    return Unknown
  case s.crystal() && s.score() >= RefresherBelow:
    return Known
  case s.crystal():
    return NotSure
  case s.implied():
    return Implied
  case s & doubtBit != 0:
    return NotSure // a gap: check before teaching or skipping
  case s.box() == 0:
    return Unknown
  case today > s.day() && int(today - s.day()) > 1 << s.box():
    return NotSure // faded
  }

  return Known
}

// Score is a crystallized concept's accuracy score (0-50), and
// whether it is crystallized at all.
func (l *Learner) Score(id know.NodeID) (int, bool) {
  if int(id) >= len(l.status) || !l.status[id].crystal() {
    return 0, false
  }

  return l.status[id].score(), true
}

// Box is a concept's Leitner box.
func (l *Learner) Box(id know.NodeID) int {
  if int(id) >= len(l.status) {
    return 0
  }

  return l.status[id].box()
}

// Refreshers lists the crystallized concepts whose score fell below
// RefresherBelow: the one thing the learner tree brings up unasked.
func (l *Learner) Refreshers() []know.NodeID {
  var out []know.NodeID
  for id, s := range l.status {
    if s.crystal() && s.score() < RefresherBelow && l.Tree.Live(know.NodeID(id)) {
      out = append(out, know.NodeID(id))
    }
  }

  return out
}
