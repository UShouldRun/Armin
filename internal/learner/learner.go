// Package learner is what the user knows: a know graph of topics and
// concepts (one per user, shared across projects) plus the events the
// advisor records about them, and the status those add up to.
//
//   learner.go  Open, Record and Close: the tree, and the events file
//               (raw 12-byte records, append-only, replayed at load)
//   status.go   the rules: Leitner boxes with fading, crystallized
//               concepts with an accuracy score, prerequisites known by
//               implication or refreshed by use; State and Refreshers
//   probe.go    finding what the user knows with few questions
//
// Status isn't stored: replaying the events rebuilds it in O(events),
// so the rules can change without migrating anything.
package learner

import (
  "bytes"
  "encoding/binary"
  "errors"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "time"
  "unsafe"

  "armin/internal/know"
)

const eventSize = 12

func init() {
  if unsafe.Sizeof(Event{}) != eventSize || unsafe.Sizeof(Status(0)) != 4 {
    panic("learner: record sizes changed; the events file depends on them")
  }
}

// Open loads the learner tree in dir and replays its events.
func Open(dir string) (*Learner, error) {
  tree, err := know.Open(dir)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  l := &Learner{ Tree: tree }
  path := filepath.Join(dir, "events")
  data, err := os.ReadFile(path)
  if err != nil && !os.IsNotExist(err) {
    tree.Close()
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  // a torn last record is dropped, and the file cut back to whole ones
  whole := len(data) / eventSize * eventSize
  r := bytes.NewReader(data[:whole])
  for {
    var e Event
    err := binary.Read(r, binary.LittleEndian, &e)
    if err == io.EOF {
      break
    }

    if err != nil {
      tree.Close()
      return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
    }

    if tree.Live(e.Node) {
      l.apply(e)
    }

    l.events = append(l.events, e)
  }

  if tree.ReadOnly() {
    return l, nil
  }

  if whole != len(data) {
    os.Truncate(path, int64(whole))
  }

  l.file, err = os.OpenFile(path, os.O_WRONLY | os.O_CREATE | os.O_APPEND, 0o600)
  if err != nil {
    tree.Close()
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  return l, nil
}

// New is an in-memory learner with no files, for tests.
func New() *Learner {
  return &Learner{ Tree: know.New() }
}

// Record appends one event and applies it.
func (l *Learner) Record(
  node know.NodeID, kind EventKind, result Result, sure Sure, when time.Time,
) error {
  if !l.Tree.Live(node) {
    return fmt.Errorf("\n\t... in Record:\n\t%d: %w", node, know.ErrNoNode)
  }

  if l.Tree.ReadOnly() {
    return know.ErrReadOnly
  }

  e := Event{
    When: uint32(when.Unix() / 60),
    Node: node,
    Bits: uint8(kind) & 3 | uint8(result) & 3 << 2 | uint8(sure) & 3 << 4,
  }

  if l.file != nil {
    var b bytes.Buffer
    binary.Write(&b, binary.LittleEndian, &e)
    _, err := l.file.Write(b.Bytes())
    if err != nil {
      return fmt.Errorf("\n\t... in Record:\n\t%w", err)
    }
  }

  // apply before append, as replay does: historyScore reads the
  // events before this one
  l.apply(e)
  l.events = append(l.events, e)

  return nil
}

func (e Event) Kind() EventKind { return EventKind(e.Bits & 3) }
func (e Event) Result() Result  { return Result(e.Bits >> 2 & 3) }
func (e Event) Sure() Sure      { return Sure(e.Bits >> 4 & 3) }

// Close syncs the events and closes the tree.
func (l *Learner) Close() error {
  var errs []error
  if l.file != nil {
    errs = append(errs, l.file.Sync(), l.file.Close())
  }

  errs = append(errs, l.Tree.Close())

  return errors.Join(errs...)
}
