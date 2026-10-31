// The words for kinds, confidence levels, flags, edge and evidence
// kinds, shared by dumps, edits and the node lines models read.

package know

var (
  kindNames = [...]string{
    "topic", "concept", "fact", "decision", "file", "symbol", "question",
  }
  confNames = [...]string{ "guess", "inferred", "checked", "tested" }
  edgeNames = [...]string{
    "requires", "implements", "explains", "contradicts", "derived-from",
  }
  refNames = [...]string{
    "file", "commit", "library", "url", "session", "check", "symbol",
  }
  roleNames = [...]string{ "user", "minion", "copilot", "advisor", "armin" }
  flagNames = [...]string{
    "stale", "suspect", "pinned", "user-edited",
    "verified", "deleted", "chosen", "cycle",
  }
)

func (k Kind) String() string     { return nameOf(kindNames[:], int(k)) }
func (c Conf) String() string     { return nameOf(confNames[:], int(c)) }
func (k EdgeKind) String() string { return nameOf(edgeNames[:], int(k)) }
func (k RefKind) String() string  { return nameOf(refNames[:], int(k)) }
func (r Role) String() string     { return nameOf(roleNames[:], int(r)) }

func nameOf(names []string, i int) string {
  if i < len(names) {
    return names[i]
  }

  return "?"
}

func indexOf(names []string, s string) (int, bool) {
  for i, n := range names {
    if n == s {
      return i, true
    }
  }

  return 0, false
}

// ParseKind, ParseConf and ParseEdge read the words back.
func ParseKind(s string) (Kind, bool) {
  i, ok := indexOf(kindNames[:], s)

  return Kind(i), ok
}

func ParseConf(s string) (Conf, bool) {
  i, ok := indexOf(confNames[:], s)

  return Conf(i), ok
}

func ParseEdge(s string) (EdgeKind, bool) {
  i, ok := indexOf(edgeNames[:], s)

  return EdgeKind(i), ok
}

func parseFlag(s string) (NodeFlags, bool) {
  i, ok := indexOf(flagNames[:], s)

  return NodeFlags(1) << i, ok
}

// FlagWords appends the names of the flags set in f to buf.
func FlagWords(f NodeFlags, buf []string) []string {
  for i, n := range flagNames {
    if f & (1 << i) != 0 {
      buf = append(buf, n)
    }
  }

  return buf
}
