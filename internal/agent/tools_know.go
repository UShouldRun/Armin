package agent

// The knowledge tree tools: what armin knows about the project, kept
// as nodes with evidence, confidence and authorship (package know).
// Results are node lines (know.Graph.Line), one per node.
//
//   know_find     search names and claims; ids read nodes in full
//   know_write    add a node, update one, or add/remove an edge
//   know_trace    what a conclusion was derived from, weakest first
//
// know_write does the work of three tools so its description rides
// on every request once, not three times: from and to name an edge
// (kind picks the edge kind, remove takes it out); id names a node
// to change (claim, kind, confidence, move_to, verified, current,
// delete); otherwise path, kind and claim add a node.
//
// A minion's writes are capped: confidence at most inferred (checked
// only with a passing acceptance check, see delegation), no decision
// nodes, no verifying.

import (
  "context"
  "fmt"
  "os"
  "slices"
  "strconv"
  "strings"

  "armin/internal/know"
  "armin/internal/llm"
)

var knowTools = []toolSpec{
  {
    id:    toolKnowFind,
    flags: flagAgent | flagSession | flagKnow,
    run:   runKnowFind,
    def: toolFunc(
      "know_find",
      "Search the knowledge tree before exploring code: what is already "+
        "known about the project, as one line per node (id, path, kind, "+
        "confidence, what it needs and what uses it, the claim). Read "+
        "code only for what it doesn't cover or marks STALE. With ids, "+
        "read those nodes in full instead: the whole claim, the evidence "+
        "and who wrote it.",
      map[string]llm.ToolProperty{
        "query": stringProp("Words to look for; a word's start matches too"),
        "under": stringProp("Only below this node: a path or an id (n42)"),
        "count": {
          Type: "integer", Description: "How many, up to 20 (default 10)",
        },
        "ids": {
          Type:        "array",
          Description: "Read these nodes in full (ids n42 or paths), " +
            "instead of searching",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
      },
      "query",
    ),
  },
  {
    id:    toolKnowWrite,
    flags: flagAgent | flagSession | flagKnow,
    run:   runKnowWrite,
    def: toolFunc(
      "know_write",
      "Add what you established to the knowledge tree, so it isn't "+
        "rediscovered — or change what is there. Search first: don't "+
        "add what a node already says, update or link it instead. "+
        "Every concept needs a definition as its claim. Put reasoning "+
        "in edges: needs (what it requires), derived_from (what you "+
        "concluded it from).",
      map[string]llm.ToolProperty{
        "path": stringProp("Add a node here, e.g. agent/loop/retries; "+
          "missing parents are added as topics"),
        "kind": {
          Type:        "string",
          Description: "What the added node is (or the node id's new kind)",
          Enum:        []string{
            "topic",
            "concept",
            "fact",
            "decision",
            "file",
            "symbol",
            "question",
          },
        },
        "claim": stringProp("What is true, in a sentence or two; a concept's " +
          "definition"),
        "confidence": {
          Type:        "string",
          Description: "guess, inferred (default), checked (you read it), " +
            "tested (a test or run showed it)",
          Enum:        []string{ "guess", "inferred", "checked", "tested" },
        },
        "evidence": {
          Type:        "array",
          Description: "What supports it: file ranges (path:12-40), URLs, " +
            "commits",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "needs": {
          Type:        "array",
          Description: "Nodes it requires (ids or paths)",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "derived_from": {
          Type:        "array",
          Description: "Nodes it was concluded from (ids or paths)",
          Items:       &llm.ToolProperty{ Type: "string" },
        },
        "force": {
          Type:        "boolean",
          Description: "Add even though a similar node exists",
        },
        "id": stringProp("Change this node instead of adding: kind, claim, " +
          "confidence, move_to, verified, current or delete"),
        "move_to":    stringProp("Move it to this new path"),
        "verified":   {
          Type: "boolean", Description: "You checked a minion's claim",
        },
        "current":    {
          Type: "boolean", Description: "Clear STALE and SUSPECT",
        },
        "delete":     {
          Type: "boolean", Description: "Delete it (no children)",
        },
        "edge": {
          Type:        "string",
          Description: "Add (or remove) an edge between two nodes " +
            "instead: from and to, this kind. requires: from needs to " +
            "before it makes sense; derived-from: from was concluded " +
            "from to; explains; implements (project code → a general " +
            "concept); contradicts",
          Enum:        []string{
            "requires", "implements", "explains", "contradicts", "derived-from",
          },
        },
        "from": stringProp("The edge goes from this node"),
        "to":   stringProp("The edge goes to this node"),
        "remove": {
          Type: "boolean", Description: "Remove the edge instead",
        },
      },
      "path", "kind", "claim",
    ),
  },
  {
    id:    toolKnowTrace,
    flags: flagAgent | flagSession | flagKnow,
    run:   runKnowTrace,
    def: toolFunc(
      "know_trace",
      "When a conclusion looks wrong: follow what it was derived from, "+
        "weakest first (unverified minion claims, low confidence, stale).",
      map[string]llm.ToolProperty{
        "id":    stringProp("Node id or path"),
        "depth": { Type: "integer", Description: "How far back (default 5)" },
      },
      "id",
    ),
  },
}

// LockKnow and UnlockKnow hold off the know tools, for closing the
// tree while a turn may still run.
func (l *Loop) LockKnow()   { l.knowMu.Lock() }
func (l *Loop) UnlockKnow() { l.knowMu.Unlock() }

// checkCycles runs the cycle pass after a write added a requires
// edge from id. A loop through id is reported in the result now, and
// remembered so the turn doesn't end with it open (see cycleCheck).
func (l *Loop) checkCycles(id know.NodeID, out map[string]string) {
  author, err := l.Know.Intern(know.RoleArmin, "", "", 0)
  if err != nil {
    return
  }

  loops, err := l.Know.FlagCycles(author)
  if err != nil {
    return
  }

  for _, loop := range loops {
    if !slices.Contains(loop, id) {
      continue
    }

    var ids []string
    for _, n := range loop {
      ids = append(ids, fmt.Sprintf("n%d", n))
    }

    out["cycle"] = "this closes a requires loop: " + strings.Join(ids, " → ") +
      ". " + cycleFix(l.Role)
    l.cyclesOpen = true
    return
  }
}

// cycleFix is how a role should deal with a loop: the minion leaves
// it to the copilot or advisor.
func cycleFix(r Role) string {
  if r == RoleMinion {
    return "Leave it: the copilot or advisor resolves loops."
  }

  return "Resolve it: merge the members into one node, flip the wrong " +
    "edge (know_write remove), or make a mutual dependency explains."
}

// OpenCycles lists the open requires loops' question nodes, as lines.
func (l *Loop) OpenCycles() string {
  dir, ok := l.Know.Lookup("cycles")
  if !ok {
    return ""
  }

  var ids []know.NodeID
  for _, q := range l.Know.Children(dir, nil) {
    if l.Know.Node(q).Flags & know.FlagCycle != 0 {
      ids = append(ids, q)
    }
  }

  return l.lines(ids, true)
}

// knowErr is the last line of a know error: its wrapping is for logs,
// not for the model.
func knowErr(err error) map[string]string {
  msg := strings.TrimSpace(err.Error())
  i := strings.LastIndex(msg, "\n")
  if i >= 0 {
    msg = strings.TrimSpace(msg[i + 1:])
  }

  return map[string]string{ "error": msg }
}

// node resolves "n42" or a path.
func (l *Loop) node(ref string) (know.NodeID, error) {
  ref = strings.TrimSpace(ref)
  if len(ref) > 1 && ref[0] == 'n' {
    v, err := strconv.ParseUint(ref[1:], 10, 32)
    if err == nil {
      if !l.Know.Live(know.NodeID(v)) {
        return 0, fmt.Errorf("no node %s", ref)
      }

      return know.NodeID(v), nil
    }
  }

  id, ok := l.Know.Lookup(strings.Trim(ref, "/"))
  if !ok {
    return 0, fmt.Errorf("no node %q", ref)
  }

  return id, nil
}

// lines renders nodes as node lines.
func (l *Loop) lines(ids []know.NodeID, full bool) string {
  var b strings.Builder
  for i, id := range ids {
    if i > 0 {
      b.WriteByte('\n')
    }

    l.Know.Line(&b, id, full)
  }

  return b.String()
}

// knowAuthor interns who is writing: this role, model and session.
func (l *Loop) knowAuthor() (know.AuthorID, error) {
  role := know.RoleCopilot
  switch l.Role {
  case RoleMinion:
    role = know.RoleMinion
  case RoleAdvisor:
    role = know.RoleAdvisor
  }

  model := ""
  if l.LLM != nil {
    model = l.LLM.Model
  }

  return l.Know.Intern(role, model, l.Session, l.Delegation)
}

func runKnowFind(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[knowArgs](c)
  if bad != nil {
    return bad
  }

  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  if len(in.IDs) > 0 {
    return knowGet(l, in.IDs)
  }

  under := know.NodeID(0)
  if in.Under != "" {
    id, err := l.node(in.Under)
    if err != nil {
      return errResult(err)
    }

    under = id
  }

  count := in.Count
  if count <= 0 {
    count = 10
  }

  hits := l.Know.Find(in.Query, under, count)
  if len(hits) == 0 {
    return map[string]string{
      "found": "nothing; explore the code, then know_write what you " +
        "establish",
    }
  }

  ids := make([]know.NodeID, len(hits))
  for i, h := range hits {
    ids[i] = h.ID
  }

  return map[string]string{ "found": l.lines(ids, false) }
}

// knowGet renders nodes in full: the whole claim, the evidence and
// the author. Called from know_find's ids argument.
func knowGet(l *Loop, refs []string) any {
  var b strings.Builder
  var evs []know.EvidenceID
  for i, ref := range refs {
    id, err := l.node(ref)
    if err != nil {
      return errResult(err)
    }

    if i > 0 {
      b.WriteString("\n\n")
    }

    l.Know.Line(&b, id, true)
    n := l.Know.Node(id)
    a := l.Know.Author(n.Author)
    fmt.Fprintf(&b, "\nby %s %s, born %s, seen %s", a.Role,
      l.Know.Str(a.Model), know.Day(n.Born).Format("2006-01-02"),
      know.Day(n.Seen).Format("2006-01-02"))
    evs = l.Know.EvidenceOf(id, evs[:0])
    for _, e := range evs {
      ev := l.Know.Ev(e)
      fmt.Fprintf(&b, "\nevidence %s %s", ev.Kind, l.Know.Str(ev.Ref))
    }
  }

  return map[string]string{ "nodes": b.String() }
}

// runKnowWrite is the one write entry point: an edge when from and
// to are set, a node change when id is set, an add otherwise. The
// three used to be three tools; one description rides on every
// request instead of three.
func runKnowWrite(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[knowArgs](c)
  if bad != nil {
    return bad
  }

  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  switch {
  case in.From != "" || in.To != "":
    return knowLink(l, in)
  case in.ID != "":
    return knowUpdate(l, in)
  default:
    return knowAdd(l, in)
  }
}

func knowAdd(l *Loop, in knowArgs) any {
  kind, ok := know.ParseKind(in.Kind)
  if !ok {
    return errResult(fmt.Errorf("unknown kind %q", in.Kind))
  }

  conf := know.ConfInferred
  if in.Confidence != "" {
    conf, ok = know.ParseConf(in.Confidence)
    if !ok {
      return errResult(fmt.Errorf("unknown confidence %q", in.Confidence))
    }
  }

  claim := ""
  if in.Claim != nil {
    claim = strings.TrimSpace(*in.Claim)
  }

  if kind == know.KindConcept && claim == "" {
    return errResult(fmt.Errorf("a concept needs its definition as the claim"))
  }

  note, err := l.minionLimits(kind, &conf)
  if err != nil {
    return errResult(err)
  }

  path := strings.Trim(in.Path, "/")
  parentPath, name := path, path
  i := strings.LastIndexByte(path, '/')
  if i >= 0 {
    parentPath, name = path[:i], path[i + 1:]
  } else {
    parentPath = ""
  }

  // resolve every reference before the first write
  var needs, from []know.NodeID
  for _, group := range []struct {
    refs []string
    out  *[]know.NodeID
  }{ { in.Needs, &needs }, { in.DerivedFrom, &from } } {
    for _, ref := range group.refs {
      id, err := l.node(ref)
      if err != nil {
        return errResult(err)
      }

      *group.out = append(*group.out, id)
    }
  }

  parent, exists := l.Know.Lookup(parentPath)
  if !in.Force {
    if exists {
      _, taken := l.Know.Child(parent, name)
      if taken {
        return errResult(fmt.Errorf("%s exists; know_write it with its id instead", path))
      }
    }

    similar := l.Know.Similar(parent, name, claim)
    if len(similar) > 0 {
      var b strings.Builder
      for i, h := range similar {
        if i > 0 {
          b.WriteByte('\n')
        }

        l.Know.Line(&b, h.ID, false)
        fmt.Fprintf(&b, " (%.2f similar)", h.Score)
      }

      return map[string]string{
        "similar": b.String(),
        "next":    "not added: know_write the existing one with id " +
          "(an edge with edge, a new claim with claim), or know_write " +
          "again with force: true if it really is different",
      }
    }
  }

  author, err := l.knowAuthor()
  if err != nil {
    return knowErr(err)
  }

  parent, err = l.ensurePath(parentPath, author)
  if err != nil {
    return knowErr(err)
  }

  id, err := l.Know.AddNode(parent, name, claim, kind, conf, author)
  if err != nil {
    return knowErr(err)
  }

  for _, ev := range in.Evidence {
    err = l.addEvidence(id, ev)
    if err != nil {
      return knowErr(err)
    }
  }

  for _, group := range []struct {
    ids  []know.NodeID
    kind know.EdgeKind
  }{ { needs, know.EdgeRequires }, { from, know.EdgeDerivedFrom } } {
    for _, to := range group.ids {
      err = l.Know.AddEdge(id, to, group.kind)
      if err != nil {
        return knowErr(err)
      }
    }
  }

  out := map[string]string{ "added": l.lines([]know.NodeID{ id }, false) }
  if note != "" {
    out["note"] = note
  }

  if len(needs) > 0 {
    l.checkCycles(id, out)
  }

  return out
}

// minionLimits caps what a minion may write: no decisions, and
// confidence at most inferred.
func (l *Loop) minionLimits(kind know.Kind, conf *know.Conf) (string, error) {
  if l.Role != RoleMinion {
    return "", nil
  }

  if kind == know.KindDecision {
    return "", fmt.Errorf("decisions belong to the copilot or the advisor")
  }

  if *conf > know.ConfInferred {
    *conf = know.ConfInferred
    return "confidence capped at inferred: a minion's claim is checked " +
      "by the copilot or advisor", nil
  }

  return "", nil
}

// ensurePath returns the node at path, adding missing levels as topics.
func (l *Loop) ensurePath(
  path string, author know.AuthorID,
) (know.NodeID, error) {
  id := know.NodeID(0)
  if path == "" {
    return id, nil
  }

  for _, part := range strings.Split(path, "/") {
    next, ok := l.Know.Child(id, part)
    if !ok {
      var err error
      next, err = l.Know.AddNode(
        id, part, "", know.KindTopic, know.ConfInferred, author,
      )
      if err != nil {
        return 0, err
      }
    }

    id = next
  }

  return id, nil
}

// addEvidence attaches one reference, telling its kind by its shape;
// a file range gets the hash of the lines it cites, for staleness.
func (l *Loop) addEvidence(id know.NodeID, ref string) error {
  ref = strings.TrimSpace(ref)
  kind := know.RefSession
  hash := uint32(0)
  switch {
  case strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://"):
    kind = know.RefURL
  case isCommit(ref):
    kind = know.RefCommit
  default:
    path, start, end, ok := know.ParseRange(ref)
    if ok {
      kind = know.RefFile
      h, err := l.rangeHash(path, start, end)
      if err != nil {
        return err
      }

      hash = h
    }
  }

  _, err := l.Know.AddEvidence(id, kind, ref, hash)

  return err
}

func isCommit(s string) bool {
  if len(s) < 7 || len(s) > 40 {
    return false
  }

  for _, c := range s {
    if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
      return false
    }
  }

  return true
}

// rangeHash hashes lines start..end (1-based, inclusive) of a project
// file.
func (l *Loop) rangeHash(path string, start, end int) (uint32, error) {
  resolved, err := l.Sandbox.ResolvePath(path)
  if err != nil {
    return 0, err
  }

  data, err := os.ReadFile(resolved)
  if err != nil {
    return 0, err
  }

  lines := know.LineHashes(data, nil)
  if end > len(lines) {
    return 0, fmt.Errorf("%s has %d lines, not %d", path, len(lines), end)
  }

  return know.RangeHash(lines[start - 1:end]), nil
}

// knowLink adds or removes an edge (know_write's from/to form).
func knowLink(l *Loop, in knowArgs) any {
  if in.From == "" || in.To == "" {
    return errResult(fmt.Errorf("an edge needs both from and to"))
  }

  from, err := l.node(in.From)
  if err != nil {
    return errResult(err)
  }

  to, err := l.node(in.To)
  if err != nil {
    return errResult(err)
  }

  kind, ok := know.ParseEdge(in.Edge)
  if !ok {
    return errResult(fmt.Errorf("unknown edge kind %q", in.Edge))
  }

  if in.Remove {
    err = l.Know.RemoveEdge(from, to, kind)
  } else {
    err = l.Know.AddEdge(from, to, kind)
  }

  if err != nil {
    return knowErr(err)
  }

  out := map[string]string{ "now": l.lines([]know.NodeID{ from }, false) }
  if kind == know.EdgeRequires && !in.Remove {
    l.checkCycles(from, out)
  } else if l.cyclesOpen {
    // a removal or reframing may have resolved one
    author, err := l.Know.Intern(know.RoleArmin, "", "", 0)
    if err == nil {
      loops, err := l.Know.FlagCycles(author)
      if err == nil && len(loops) == 0 {
        l.cyclesOpen = false
        out["cycle"] = "no requires loops are open now"
      }
    }
  }

  return out
}

// knowUpdate changes or deletes a node (know_write's id form).
func knowUpdate(l *Loop, in knowArgs) any {
  id, err := l.node(in.ID)
  if err != nil {
    return errResult(err)
  }

  if in.Delete {
    err = l.Know.Delete(id)
    if err != nil {
      return knowErr(err)
    }

    return map[string]string{ "deleted": in.ID }
  }

  if in.Verified && l.Role == RoleMinion {
    return errResult(fmt.Errorf("only the copilot or the advisor verifies"))
  }

  n := l.Know.Node(id)
  kind, conf := n.Kind(), n.Conf()
  if in.Kind != "" {
    k, ok := know.ParseKind(in.Kind)
    if !ok {
      return errResult(fmt.Errorf("unknown kind %q", in.Kind))
    }

    kind = k
  }

  if in.Confidence != "" {
    cf, ok := know.ParseConf(in.Confidence)
    if !ok {
      return errResult(fmt.Errorf("unknown confidence %q", in.Confidence))
    }

    conf = cf
  }

  note, err := l.minionLimits(kind, &conf)
  if err != nil {
    return errResult(err)
  }

  if kind != n.Kind() || conf != n.Conf() {
    err = l.Know.SetKind(id, kind, conf)
    if err != nil {
      return knowErr(err)
    }
  }

  if in.Claim != nil {
    claim := strings.TrimSpace(*in.Claim)
    if kind == know.KindConcept && claim == "" {
      return errResult(
        fmt.Errorf("a concept needs its definition as the claim"),
      )
    }

    err = l.Know.SetClaim(id, claim)
    if err != nil {
      return knowErr(err)
    }
  }

  if in.MoveTo != "" {
    path := strings.Trim(in.MoveTo, "/")
    parentPath, name := "", path
    i := strings.LastIndexByte(path, '/')
    if i >= 0 {
      parentPath, name = path[:i], path[i + 1:]
    }

    author, err := l.knowAuthor()
    if err != nil {
      return knowErr(err)
    }

    parent, err := l.ensurePath(parentPath, author)
    if err != nil {
      return knowErr(err)
    }

    err = l.Know.Move(id, parent, name)
    if err != nil {
      return knowErr(err)
    }
  }

  var set, clear know.NodeFlags
  if in.Verified {
    set |= know.FlagVerified
  }

  if in.Current {
    clear |= know.FlagStale | know.FlagSuspect
  }

  err = l.Know.SetFlags(id, set, clear)
  if err != nil {
    return knowErr(err)
  }

  err = l.Know.Touch(id)
  if err != nil {
    return knowErr(err)
  }

  out := map[string]string{ "now": l.lines([]know.NodeID{ id }, false) }
  if note != "" {
    out["note"] = note
  }

  return out
}

func runKnowTrace(_ context.Context, l *Loop, c toolCall) any {
  in, bad := decode[knowArgs](c)
  if bad != nil {
    return bad
  }

  l.knowMu.Lock()
  defer l.knowMu.Unlock()

  id, err := l.node(in.ID)
  if err != nil {
    return errResult(err)
  }

  depth := in.Depth
  if depth <= 0 {
    depth = 5
  }

  steps := l.Know.Trace(id, depth)
  if len(steps) == 0 {
    return map[string]string{ "trace": "nothing: it has no derived-from edges" }
  }

  var b strings.Builder
  for i, s := range steps {
    if i > 0 {
      b.WriteByte('\n')
    }

    fmt.Fprintf(&b, "depth %d: ", s.Depth)
    l.Know.Line(&b, s.ID, false)
  }

  return map[string]string{ "trace": b.String() }
}
