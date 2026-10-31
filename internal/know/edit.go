// Loading an edited dump back. Each block is the state its node should
// be in: a changed path moves or renames it, a missing claim line
// clears the claim, the flags and edge lines replace the node's flags
// and out edges. "new <path> <kind> <conf>" blocks add nodes, whose
// parents must exist or come earlier in the file. Nodes not in the
// file are left alone; the deleted flag deletes one. Every node the
// edit changes is flagged user-edited.
//
// Everything is parsed and checked before the first change; after
// that, changes apply in order: new nodes, existing nodes, edges,
// deletions (deepest first).

package know

import (
  "bufio"
  "fmt"
  "io"
  "slices"
  "strconv"
  "strings"
)

// editable are the flags an edit sets and clears; deleted is handled
// apart, user-edited is set by the edit itself.
const editable = FlagAll &^ (FlagDeleted | FlagUserEdited)

// Edit applies an edited dump read from r; author is credited with
// the nodes it adds.
func (g *Graph) Edit(r io.Reader, author AuthorID) (EditResult, error) {
  var res EditResult
  blocks, err := parseEdit(r)
  if err != nil {
    return res, fmt.Errorf("\n\t... in Edit:\n\t%w", err)
  }

  seen := map[NodeID]bool{}
  for _, b := range blocks {
    if b.isNew {
      continue
    }

    if !g.live(b.id) || b.id == 0 {
      return res, fmt.Errorf(
        "\n\t... in Edit:\n\tline %d: no node n%d", b.line, b.id,
      )
    }

    if seen[b.id] {
      return res, fmt.Errorf(
        "\n\t... in Edit:\n\tline %d: n%d appears twice", b.line, b.id,
      )
    }

    seen[b.id] = true
  }

  for _, b := range blocks {
    if !b.isNew {
      continue
    }

    parent, name := splitPath(b.path)
    p, ok := g.Lookup(parent)
    if !ok {
      return res, fmt.Errorf(
        "\n\t... in Edit:\n\tline %d: no parent %q for a new node", b.line,
        parent,
      )
    }

    b.id, err = g.AddNode(p, name, b.claim, b.kind, b.conf, author)
    if err != nil {
      return res, fmt.Errorf("\n\t... in Edit:\n\tline %d: %w", b.line, err)
    }

    err = g.SetFlags(b.id, b.flags & editable | FlagUserEdited, 0)
    if err != nil {
      return res, err
    }

    res.Added++
  }

  changed := map[NodeID]bool{}
  for _, b := range blocks {
    if b.isNew {
      continue
    }

    ch, err := g.editNode(b)
    if err != nil {
      return res, fmt.Errorf("\n\t... in Edit:\n\tline %d: %w", b.line, err)
    }

    if ch {
      changed[b.id] = true
    }
  }

  var have []Edge
  for _, b := range blocks {
    ch, err := g.editEdges(b, have[:0])
    if err != nil {
      return res, fmt.Errorf("\n\t... in Edit:\n\tline %d: %w", b.line, err)
    }

    if ch && !b.isNew {
      changed[b.id] = true
    }
  }

  var doomed []NodeID
  for _, b := range blocks {
    if !b.isNew && b.flags & FlagDeleted != 0 {
      doomed = append(doomed, b.id)
      delete(changed, b.id)
    }
  }

  for id := range changed {
    err = g.SetFlags(id, FlagUserEdited, 0)
    if err != nil {
      return res, err
    }

    res.Changed++
  }

  depth := func(id NodeID) int {
    d := 0
    for ; id != 0; id = g.node(id).Parent {
      d++
    }

    return d
  }

  slices.SortFunc(doomed, func(a, b NodeID) int { return depth(b) - depth(a) })
  for _, id := range doomed {
    err = g.Delete(id)
    if err != nil {
      return res, fmt.Errorf("\n\t... in Edit:\n\t%w", err)
    }

    res.Deleted++
  }

  return res, nil
}

// editNode brings an existing node's path, kind, claim and flags to
// the block's.
func (g *Graph) editNode(b *editBlock) (bool, error) {
  ch := false
  if g.Path(b.id) != b.path {
    parent, name := splitPath(b.path)
    p, ok := g.Lookup(parent)
    if !ok {
      return false, fmt.Errorf("no parent %q", parent)
    }

    err := g.Move(b.id, p, name)
    if err != nil {
      return false, err
    }

    ch = true
  }

  n := g.node(b.id)
  if n.Kind() != b.kind || n.Conf() != b.conf {
    err := g.SetKind(b.id, b.kind, b.conf)
    if err != nil {
      return false, err
    }

    ch = true
  }

  if g.Claim(b.id) != b.claim {
    err := g.SetClaim(b.id, b.claim)
    if err != nil {
      return false, err
    }

    ch = true
  }

  want := b.flags & editable
  have := g.node(b.id).Flags & editable
  if want != have {
    err := g.SetFlags(b.id, want, have &^ want)
    if err != nil {
      return false, err
    }

    ch = true
  }

  return ch, nil
}

// editEdges makes a node's out edges exactly the block's.
func (g *Graph) editEdges(b *editBlock, have []Edge) (bool, error) {
  var want []Edge
  for k, targets := range b.edges {
    for _, t := range targets {
      to, ok := g.target(t)
      if !ok {
        return false, fmt.Errorf("no node %q", t)
      }

      want = append(want, Edge{ To: to, Kind: EdgeKind(k) })
    }
  }

  ch := false
  have = g.Out(b.id, have)
  for _, e := range have {
    if !slices.Contains(want, e) {
      err := g.RemoveEdge(b.id, e.To, e.Kind)
      if err != nil {
        return false, err
      }

      ch = true
    }
  }

  for _, e := range want {
    if !slices.Contains(have, e) {
      err := g.AddEdge(b.id, e.To, e.Kind)
      if err != nil {
        return false, err
      }

      ch = true
    }
  }

  return ch, nil
}

// target resolves "n42" or a path.
func (g *Graph) target(t string) (NodeID, bool) {
  id, ok := parseID(t)
  if ok {
    return id, g.live(id)
  }

  return g.Lookup(t)
}

func parseID(t string) (NodeID, bool) {
  if len(t) < 2 || t[0] != 'n' {
    return 0, false
  }

  v, err := strconv.ParseUint(t[1:], 10, 29)
  if err != nil {
    return 0, false
  }

  return NodeID(v), true
}

func splitPath(path string) (string, string) {
  i := strings.LastIndexByte(path, '/')
  if i < 0 {
    return "", path
  }

  return path[:i], path[i + 1:]
}

// parseEdit reads the blocks of an edited dump.
func parseEdit(r io.Reader) ([]*editBlock, error) {
  var blocks []*editBlock
  var cur *editBlock
  sc := bufio.NewScanner(r)
  sc.Buffer(nil, 1 << 20)
  line := 0
  for sc.Scan() {
    line++
    text := sc.Text()
    trimmed := strings.TrimSpace(text)
    if trimmed == "" || trimmed[0] == '#' {
      continue
    }

    f, err := fields(trimmed)
    if err != nil {
      return nil, fmt.Errorf("line %d: %w", line, err)
    }

    indented := text[0] == ' ' || text[0] == '\t'
    if !indented {
      if f[0] == "author" {
        cur = nil
        continue
      }

      cur, err = parseHeader(f, line)
      if err != nil {
        return nil, err
      }

      blocks = append(blocks, cur)
      continue
    }

    if cur == nil {
      return nil, fmt.Errorf("line %d: an attribute outside a node block", line)
    }

    err = cur.attr(f)
    if err != nil {
      return nil, fmt.Errorf("line %d: %w", line, err)
    }
  }

  return blocks, sc.Err()
}

// parseHeader reads "n42 path kind conf …" or "new path kind conf".
func parseHeader(f []string, line int) (*editBlock, error) {
  if len(f) < 4 {
    return nil, fmt.Errorf("line %d: want \"n<id> <path> <kind> <conf>\"", line)
  }

  b := &editBlock{ line: line, path: f[1] }
  if f[0] == "new" {
    b.isNew = true
  } else {
    id, ok := parseID(f[0])
    if !ok {
      return nil, fmt.Errorf(
        "line %d: %q is not a node ID or \"new\"", line, f[0],
      )
    }

    b.id = id
  }

  if b.path == "" || strings.HasPrefix(b.path, "/") ||
      strings.HasSuffix(b.path, "/") {
    return nil, fmt.Errorf("line %d: bad path %q", line, b.path)
  }

  for _, part := range strings.Split(b.path, "/") {
    err := validName(part)
    if err != nil {
      return nil, fmt.Errorf("line %d: %w", line, err)
    }
  }

  var ok bool
  b.kind, ok = ParseKind(f[2])
  if !ok {
    return nil, fmt.Errorf("line %d: unknown kind %q", line, f[2])
  }

  b.conf, ok = ParseConf(f[3])
  if !ok {
    return nil, fmt.Errorf("line %d: unknown confidence %q", line, f[3])
  }

  return b, nil
}

func (b *editBlock) attr(f []string) error {
  switch f[0] {
  case "claim":
    if len(f) != 2 {
      return fmt.Errorf("want claim \"…\"")
    }

    b.claim = f[1]

  case "flags":
    for _, w := range f[1:] {
      fl, ok := parseFlag(w)
      if !ok {
        return fmt.Errorf("unknown flag %q", w)
      }

      b.flags |= fl
    }

  case "evidence":

  default:
    k, ok := ParseEdge(f[0])
    if !ok {
      return fmt.Errorf("unknown line %q", f[0])
    }

    b.edges[k] = append(b.edges[k], f[1:]...)
  }

  return nil
}

// fields splits on spaces; a token starting with a double quote is a
// Go-quoted string and comes back unquoted.
func fields(s string) ([]string, error) {
  var out []string
  for {
    s = strings.TrimLeft(s, " \t")
    if s == "" {
      return out, nil
    }

    if s[0] == '"' {
      q, err := strconv.QuotedPrefix(s)
      if err != nil {
        return nil, fmt.Errorf("bad quoted string")
      }

      v, _ := strconv.Unquote(q)
      out = append(out, v)
      s = s[len(q):]
      continue
    }

    i := strings.IndexAny(s, " \t")
    if i < 0 {
      i = len(s)
    }

    out = append(out, s[:i])
    s = s[i:]
  }
}
