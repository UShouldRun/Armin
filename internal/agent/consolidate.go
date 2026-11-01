// Consolidation: turning the pointers armin records after each turn
// (recordTurn: a `file` node under sessions/ whose claim is a list of
// file:line refs) into claims a later session can answer from. The
// tree benchmark (round 8) showed the difference: a third session
// saved 56% where the first had left a model-written claim, 30% where
// it had left only a pointer.
//
// It runs off the turn, in the background at session start, on the
// minion's model (the local GPU), a few pointers per session:
//
//   Consolidate      the pass: pending pointers, oldest first
//   consolidateOne   one pointer: read its ranges, ask for claims,
//                    keep those citing a range shown, write them
//
// A pointer is pending while no node is derived from it. Claims are
// the minion's, inferred and unverified, with the cited range as
// hash-checked evidence; a claim a similar node already makes links
// to that node instead of repeating it.

package agent

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "regexp"
  "strings"
  "time"

  "armin/internal/know"
  "armin/internal/llm"
  "armin/internal/metrics"
)

const (
  // consolidateLines caps the code shown for one pointer.
  consolidateLines = 240

  // consolidateClaims caps the claims written for one pointer.
  consolidateClaims = 3
)

const consolidatePrompt = "You turn a note into facts. You get a question " +
  "someone asked about a codebase and the code that answered it, as " +
  "excerpts headed by path:start-end. Write at most three short facts " +
  "that answer the question and the follow-up most likely after it: " +
  "what the code does and how, not where it is. Each on its own line, " +
  "ending with \" @ \" and the one excerpt header it rests on, exactly " +
  "as given. Only what the excerpts show. Nothing else in the reply."

// claimLine is "the fact @ path:start-end".
var claimLine = regexp.MustCompile(`^\s*[-*]?\s*(.+?)\s+@\s+(\S+:\d+-\d+)\s*$`)

// Consolidate turns up to max pending pointers into claims with the
// minion's model, and reports how many pointers it consolidated.
func (l *Loop) Consolidate(ctx context.Context, max int) (int, error) {
  if l.Know == nil || l.Minion == nil || l.Know.ReadOnly() {
    return 0, nil
  }

  start := time.Now()
  defer metrics.Since("know.consolidate.ms", start)

  client, err := l.Minion(ctx)
  if err != nil {
    return 0, fmt.Errorf("\n\t... in Consolidate:\n\t%w", err)
  }

  // a local thinking model spent the whole 1024 on reasoning and
  // replied empty (48 of 54 on armin-test): no thinking for local
  chore := *client.Chore()
  chore.MaxTokens = 1024
  if chore.APIKey == "" {
    chore.Effort = "none"
  }

  done := 0
  for _, id := range l.pendingPointers(max) {
    if ctx.Err() != nil {
      break
    }

    ok, err := l.consolidateOne(ctx, &chore, id)
    if err != nil {
      metrics.Add("know.consolidate.failed", 1)
      metrics.Event("know.consolidate.failed", "node", id, "error", err)
      continue
    }

    if ok {
      done++
    }
  }

  return done, nil
}

// pendingPointers lists up to max pointers under sessions/ that no node
// is derived from yet, oldest first.
func (l *Loop) pendingPointers(max int) []know.NodeID {
  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  dir, ok := l.Know.Lookup("sessions")
  if !ok {
    return nil
  }

  var out []know.NodeID
  var in []know.Edge
  for _, id := range l.Know.Children(dir, nil) {
    n := l.Know.Node(id)
    if n.Kind() != know.KindFile || n.Flags & know.FlagStale != 0 {
      continue
    }

    derived := false
    in = l.Know.In(id, in[:0])
    for _, e := range in {
      if e.Kind == know.EdgeDerivedFrom {
        derived = true
      }
    }

    if !derived {
      out = append(out, id)
    }

    if len(out) == max {
      break
    }
  }

  metrics.Add("know.consolidate.pending", int64(len(out)))

  return out
}

// consolidateOne turns one pointer into claims; false when there was
// nothing to write (no readable ranges, no usable claim).
func (l *Loop) consolidateOne(
  ctx context.Context, client *llm.Client, id know.NodeID,
) (bool, error) {
  question, excerpts := l.pointerSource(id)
  if len(excerpts) == 0 {
    return false, nil
  }

  var user strings.Builder
  fmt.Fprintf(&user, "Question: %s\n", question)
  for _, e := range excerpts {
    fmt.Fprintf(&user, "\n%s\n%s\n", e.header, e.text)
  }

  reply, err := client.Chat(ctx, []llm.Message{
    { Role: "system", Content: consolidatePrompt },
    { Role: "user", Content: user.String() },
  }, nil, nil)
  if err != nil {
    return false, err
  }

  // what the pass itself costs, so a benchmark can charge it
  metrics.Observe("know.consolidate.in", float64(reply.Usage.PromptTokens))
  metrics.Observe("know.consolidate.out", float64(reply.Usage.CompletionTokens))
  metrics.Observe("know.consolidate.cost", reply.Usage.Cost)

  shown := map[string]bool{}
  for _, e := range excerpts {
    shown[e.header] = true
  }

  var claims []claim
  for _, line := range strings.Split(reply.Content, "\n") {
    m := claimLine.FindStringSubmatch(line)
    if m == nil || !shown[m[2]] || len(claims) == consolidateClaims {
      continue
    }

    claims = append(claims, claim{ text: strings.TrimSpace(m[1]), ref: m[2] })
  }

  if len(claims) == 0 {
    metrics.Add("know.consolidate.empty", 1)
    return false, nil
  }

  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  author, err := l.Know.Intern(know.RoleMinion, client.Model, l.Session, 0)
  if err != nil {
    return false, err
  }

  for i, c := range claims {
    err = l.writeClaim(id, i, c.text, c.ref, author)
    if err != nil {
      return false, err
    }
  }

  metrics.Add("know.consolidate.pointers", 1)
  metrics.Event("know.consolidate", "node", id, "claims", len(claims))

  return true, nil
}

// writeClaim adds one consolidated claim under the cited file's
// package, derived from the pointer; a similar existing node gets the
// link instead.
func (l *Loop) writeClaim(
  pointer know.NodeID, i int, text, ref string, author know.AuthorID,
) error {
  // the cited range must hash before anything is written, or a
  // claim would be left with no evidence and no pointer
  path, start, end, _ := know.ParseRange(ref)
  _, err := l.rangeHash(path, start, end)
  if err != nil {
    return err
  }

  area := filepath.Base(filepath.Dir(path))
  if area == "." || area == "/" {
    area = "project"
  }

  parent, err := l.ensurePath(area, author)
  if err != nil {
    return err
  }

  // compared on the claim alone: the pointer's name is the question's
  // words, which would dilute the overlap with an existing claim
  base := l.Know.Name(pointer)
  similar := l.Know.Similar(parent, "", text)
  if len(similar) > 0 {
    metrics.Add("know.consolidate.linked", 1)
    return l.Know.AddEdge(similar[0].ID, pointer, know.EdgeDerivedFrom)
  }

  // the pointer's name, or the next free "-2", "-3"...
  name := base
  for n := 2 + i; ; n++ {
    _, taken := l.Know.Child(parent, name)
    if !taken {
      break
    }

    name = fmt.Sprintf("%s-%d", base, n)
  }

  id, err := l.Know.AddNode(
    parent, name, text, know.KindFact, know.ConfInferred, author,
  )
  if err != nil {
    return err
  }

  err = l.addEvidence(id, ref)
  if err != nil {
    return err
  }

  metrics.Add("know.consolidate.claims", 1)

  return l.Know.AddEdge(id, pointer, know.EdgeDerivedFrom)
}

// pointerSource is a pointer's question (from its session evidence)
// and the current text of its cited ranges that are still unchanged,
// up to consolidateLines lines.
func (l *Loop) pointerSource(id know.NodeID) (string, []excerpt) {
  l.knowMu.Lock()
  question := l.Know.Name(id)
  var refs []string
  for _, e := range l.Know.EvidenceOf(id, nil) {
    ev := l.Know.Ev(e)
    ref := l.Know.Str(ev.Ref)
    switch {
    case ev.Kind == know.RefSession && strings.HasPrefix(ref, questionPrefix):
      question = strings.TrimPrefix(ref, questionPrefix)
    case ev.Kind == know.RefFile:
      refs = append(refs, ref)
    case ev.Kind == know.RefSession && !strings.ContainsAny(ref, " :"):
      // a whole-file read: its first lines
      refs = append(refs, fmt.Sprintf("%s:1-%d", ref, consolidateLines))
    }
  }

  l.knowMu.Unlock()

  var out []excerpt
  lines := 0
  for _, ref := range refs {
    path, start, end, ok := know.ParseRange(ref)
    if !ok || lines >= consolidateLines {
      continue
    }

    resolved, err := l.Sandbox.ResolvePath(path)
    if err != nil {
      continue
    }

    data, err := os.ReadFile(resolved)
    if err != nil {
      continue
    }

    // a final newline ends the last line; it doesn't start another
    all := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
    end = min(end, len(all), start - 1 + consolidateLines - lines)
    if start > end {
      continue
    }

    out = append(out, excerpt{
      header: fmt.Sprintf("%s:%d-%d", path, start, end),
      text:   strings.Join(all[start - 1:end], "\n"),
    })
    lines += end - start + 1
  }

  return question, out
}
