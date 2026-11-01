// Recall: armin searches the knowledge DAG with the user's message
// before the model sees it, and puts the closest nodes above that
// message. Reading the tree then costs the model no tool round and no
// read tool's description on every request; the system prompt stays
// byte-stable, since only the new message changes.
//
//   recall        at the start of a turn: search, attach, remember
//   recallSettle  at its end: did the model cite what was attached
//                 ([nN]), or only read a recalled node's file?

package agent

import (
  "fmt"
  "path/filepath"
  "slices"
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

  recallHead = "[armin] From the knowledge DAG (cite as [nN]):\n"
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

// recallSettle counts whether the turn used what recall attached. A
// citation of one of the nodes is the only sure sign: [nN] in an
// answer, or a know_write building on the node. Reading a recalled
// node's file is counted apart, since the model often reads that file
// anyway.
func (l *Loop) recallSettle(history []llm.Message, from int) {
  if len(l.recalled) == 0 {
    return
  }

  // the nodes a know_write in the turn named (id, from, to, needs,
  // derived_from): building on a recalled node is citing it
  var named []string
  for _, m := range history[min(from, len(history)):] {
    for _, c := range m.ToolCalls {
      if c.Function.Name == "know_write" {
        named = knowWriteNames(c.Function.Arguments, named)
      }
    }
  }

  cited := 0
  read := false
  for _, id := range l.recalled {
    l.knowMu.Lock()
    path := l.Know.Path(id)
    l.knowMu.Unlock()

    short := fmt.Sprintf("n%d", id)
    tag := "[" + short + "]"
    used := slices.Contains(named, short) || slices.Contains(named, path)
    for _, m := range history[min(from, len(history)):] {
      if m.Role == "assistant" && strings.Contains(m.Content, tag) {
        used = true
      }
    }

    if used {
      cited++
    }
  }

  for _, m := range history[min(from, len(history)):] {
    for _, c := range m.ToolCalls {
      p, _ := c.Function.Arguments["path"].(string)
      if p != "" && slices.Contains(l.recalledFiles, filepath.Clean(p)) {
        read = true
      }
    }
  }

  switch {
  case cited > 0:
    metrics.Add("recall.cited", 1)
    metrics.Observe("recall.cited.nodes", float64(cited))
  case read:
    metrics.Add("recall.read", 1)
  }

  metrics.Event("recall.settle",
    "cited", cited, "read", read, "nodes", len(l.recalled))
}

// knowWriteNames appends the node references in a know_write call's
// arguments to out.
func knowWriteNames(args map[string]any, out []string) []string {
  for _, key := range []string{ "id", "from", "to" } {
    v, _ := args[key].(string)
    if v != "" {
      out = append(out, v)
    }
  }

  for _, key := range []string{ "needs", "derived_from" } {
    list, _ := args[key].([]any)
    for _, v := range list {
      s, _ := v.(string)
      if s != "" {
        out = append(out, s)
      }
    }
  }

  return out
}
