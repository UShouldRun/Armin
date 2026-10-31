// Recall: armin searches the knowledge tree with the user's message
// before the model sees it, and puts the closest nodes above that
// message. Reading the tree then costs the model no tool round and no
// read tool's description on every request; the system prompt stays
// byte-stable, since only the new message changes.
//
//   recall        at the start of a turn: search, attach, remember
//   recallSettle  at its end: did the model use what was attached
//                 (cite [nN], or read a recalled node's at-ref file)?

package agent

import (
  "fmt"
  "path/filepath"
  "strings"

  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/metrics"
)

const (
  // recallHits caps the nodes attached to one message.
  recallHits = 3

  // recallFloor drops hits scoring under this share of the best one:
  // a weak match is noise the model would have to read and discard.
  recallFloor = 0.4

  recallHead = "[armin] From the knowledge tree (cite as [nN]):\n"
)

// recall attaches the tree's best matches for the turn's user message.
func (l *Loop) recall(history []llm.Message) {
  l.recalled, l.recalledFiles = l.recalled[:0], l.recalledFiles[:0]
  if !l.Recall || l.Know == nil || len(history) == 0 {
    return
  }

  last := &history[len(history) - 1]
  if last.Role != "user" {
    return
  }

  metrics.Add("recall.turns", 1)
  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  hits := l.Know.Find(last.Content, 0, recallHits)
  var ids []know.NodeID
  for _, h := range hits {
    if h.Score >= recallFloor * hits[0].Score {
      ids = append(ids, h.ID)
    }
  }

  if len(ids) == 0 {
    metrics.Add("recall.empty", 1)
    return
  }

  var evs []know.EvidenceID
  for _, id := range ids {
    evs = l.Know.EvidenceOf(id, evs[:0])
    for _, e := range evs {
      path, _, _, ok := know.ParseRange(l.Know.Str(l.Know.Ev(e).Ref))
      if ok {
        l.recalledFiles = append(l.recalledFiles, path)
      }
    }
  }

  l.recalled = append(l.recalled, ids...)
  last.Content = recallHead + l.lines(ids, false) + "\n\n" + last.Content
  metrics.Add("recall.attached", 1)
  metrics.Observe("recall.nodes", float64(len(ids)))
  metrics.Event("recall.attach",
    "nodes", len(ids), "top", fmt.Sprintf("n%d", ids[0]))
}

// recallSettle counts whether the turn used what recall attached: an
// answer citing one of the nodes, or a read of a recalled node's file.
func (l *Loop) recallSettle(history []llm.Message, from int) {
  if len(l.recalled) == 0 {
    return
  }

  used := false
  for _, m := range history[min(from, len(history)):] {
    if m.Role != "assistant" {
      continue
    }

    for _, id := range l.recalled {
      if strings.Contains(m.Content, fmt.Sprintf("[n%d]", id)) {
        used = true
      }
    }

    for _, c := range m.ToolCalls {
      p, _ := c.Function.Arguments["path"].(string)
      for _, f := range l.recalledFiles {
        if p != "" && filepath.Clean(p) == f {
          used = true
        }
      }
    }
  }

  if used {
    metrics.Add("recall.used", 1)
  }

  metrics.Event("recall.settle", "used", used, "nodes", len(l.recalled))
}
