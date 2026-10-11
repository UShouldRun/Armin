// Package ctags gives armin deterministic knowledge of a codebase's
// structure — where a function, type, or variable is actually
// defined — by shelling out to Universal Ctags, the same way
// internal/tools wraps ripgrep. This is what backs the LSP server's
// real (non-LLM) textDocument/definition and workspace/symbol, and
// grounds hover answers in a fact instead of a guess.
//
// Enhancements over calling ctags raw:
//   - excludes vendor/build noise the same way list_directory does
//   - restricts languages to what armin actually targets, to keep a
//     large repo's index fast to build
//   - caches the index with a short TTL instead of re-scanning on
//     every request
//   - ranks same-file and same-directory matches first when a name
//     is ambiguous, instead of returning ctags' flat, unordered list
//   - gives kind letters ("f", "s", "m", ...) human labels for
//     prompts
package ctags

import (
  "context"
  "fmt"
  "os/exec"
  "path/filepath"
  "strconv"
  "strings"
  "sync"
  "time"
)

// DefaultLanguages restricts indexing to the languages armin is
// meant to understand; narrower than ctags' full ~170-language
// list, which keeps a large repo's index fast to build.
var DefaultLanguages = []string{
  "C", "C++", "Go", "JavaScript", "TypeScript", "Python",
}

// excludeDirs mirrors internal/tools/listdir.go's skipDirs — the
// same noise directories are worth keeping out of a symbol index
// for the same reason: signal density, and here also just plain
// build speed.
var excludeDirs = []string{
  ".git", "node_modules", "vendor", ".idea", ".vscode", "dist", "build",
}

// staleAfter bounds how long a built index is trusted before the
// next lookup triggers a rebuild. Ctags is fast enough on real
// projects that a short TTL keeps results fresh without re-scanning
// on every keystroke-driven request.
const staleAfter = 5 * time.Second

// Symbol is one definition site, as reported by ctags.
type Symbol struct {
  Name string
  Kind string // single-letter ctags kind, language-dependent
  File string // relative to the index's root
  Line int
  // Scope is e.g. "struct:agent.Loop" or "package:agent"; "" if none.
  Scope string
}

// Index is a lazily-built, TTL-cached, name -> definitions map for
// one project root. Safe for concurrent use.
type Index struct {
  root, binary string

  mu      sync.Mutex
  byName  map[string][]Symbol
  builtAt time.Time
  lastErr error
}

// New returns an Index for root. Nothing runs until the first
// Lookup or Search call — building the index has a real cost, so it
// shouldn't happen just because the LSP server started.
func New(root, binary string) *Index {
  return &Index{
    root: root, binary: binary, byName: make(map[string][]Symbol),
  }
}

// Lookup returns every known definition of name, ranked so a match
// in the same file as fromFile comes first, then a match in the
// same directory, then everything else — ctags itself returns an
// unordered flat list, so this is armin's one added judgment call
// on top of it. fromFile may be "" to skip ranking.
func (idx *Index) Lookup(
  ctx context.Context, name, fromFile string,
) []Symbol {
  idx.ensureFresh(ctx)

  idx.mu.Lock()
  matches := append([]Symbol{}, idx.byName[name]...)
  idx.mu.Unlock()

  rankSymbols(matches, fromFile)
  return matches
}

// Search returns every symbol whose name contains query (case-
// insensitive) — workspace/symbol wants fuzzy-ish matching, not an
// exact key lookup.
func (idx *Index) Search(ctx context.Context, query string) []Symbol {
  idx.ensureFresh(ctx)

  idx.mu.Lock()
  defer idx.mu.Unlock()

  var matches []Symbol
  for name, syms := range idx.byName {
    // containsFold, not strings.Contains(strings.ToLower(name),
    // ...): this runs once per symbol in the whole index on every
    // call, and ToLower would allocate a new string for every one.
    if query == "" || containsFold(name, query) {
      matches = append(matches, syms...)
    }
  }
  return matches
}

// containsFold is an allocation-free, ASCII-case-insensitive
// strings.Contains. Identifiers are overwhelmingly ASCII, so this
// covers the real case without pulling in unicode.ToLower's cost.
func containsFold(s, substr string) bool {
  if len(substr) == 0 {
    return true
  }
  if len(substr) > len(s) {
    return false
  }
  for i := 0; i + len(substr) <= len(s); i++ {
    if hasPrefixFold(s[i:], substr) {
      return true
    }
  }
  return false
}

func hasPrefixFold(s, prefix string) bool {
  for i := 0; i < len(prefix); i++ {
    if lowerASCII(s[i]) != lowerASCII(prefix[i]) {
      return false
    }
  }
  return true
}

func lowerASCII(b byte) byte {
  if b >= 'A' && b <= 'Z' {
    return b + ('a' - 'A')
  }
  return b
}

// LastError reports the most recent rebuild failure, if any —
// useful for surfacing "ctags isn't installed" once instead of on
// every call.
func (idx *Index) LastError() error {
  idx.mu.Lock()
  defer idx.mu.Unlock()
  return idx.lastErr
}

func (idx *Index) ensureFresh(ctx context.Context) {
  idx.mu.Lock()
  defer idx.mu.Unlock()

  if time.Since(idx.builtAt) < staleAfter {
    return
  }

  byName, err := scan(ctx, idx.root, idx.binary)
  idx.lastErr = err
  if err != nil {
    return // keep serving the previous (possibly empty) index
  }

  idx.byName = byName
  idx.builtAt = time.Now()
}

func scan(
  ctx context.Context, root, binary string,
) (map[string][]Symbol, error) {
  args := []string{
    "-f", "-",
    "--excmd=number",
    "--fields=+n",
    "-R",
    "--languages=" + strings.Join(DefaultLanguages, ","),
  }
  for _, dir := range excludeDirs {
    args = append(args, "--exclude=" + dir)
  }
  args = append(args, ".")

  cmd := exec.CommandContext(ctx, binary, args...)
  cmd.Dir = root

  out, err := cmd.Output()
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in scan:\n\t running %s: %w", binary, err,
    )
  }

  byName := make(map[string][]Symbol)
  for _, line := range strings.Split(string(out), "\n") {
    sym, ok := parseLine(line)
    if !ok {
      continue
    }
    byName[sym.Name] = append(byName[sym.Name], sym)
  }
  return byName, nil
}

// parseLine reads one line of Universal Ctags' extended tag format:
//
//	name\tfile\tline;"\tkind\tline:N\t[scope]\t[typeref]...
//
// --excmd=number (see scan) guarantees the third column is a bare
// line number rather than a search pattern — a source line indented
// with a literal tab would otherwise land inside that field and
// break a naive tab-split.
func parseLine(line string) (Symbol, bool) {
  if line == "" || strings.HasPrefix(line, "!_TAG_") {
    return Symbol{}, false
  }

  cols := strings.Split(line, "\t")
  if len(cols) < 4 {
    return Symbol{}, false
  }

  sym := Symbol{
    Name: cols[0], File: filepath.ToSlash(cols[1]), Kind: cols[3],
  }

  for _, field := range cols[4:] {
    key, value, hasKey := strings.Cut(field, ":")
    if !hasKey {
      continue
    }
    switch key {
    case "line":
      if n, err := strconv.Atoi(value); err == nil {
        sym.Line = n
      }
    case "struct", "class", "interface", "package", "namespace":
      sym.Scope = key + ":" + value
    }
  }

  if sym.Line == 0 {
    trimmed := strings.TrimSuffix(cols[2], `;"`)
    if n, err := strconv.Atoi(trimmed); err == nil {
      sym.Line = n
    }
  }

  return sym, true
}

// rankSymbols sorts matches in place: same file as fromFile first,
// then same directory, then everything else, each group otherwise
// left in ctags' original order.
func rankSymbols(matches []Symbol, fromFile string) {
  if fromFile == "" || len(matches) < 2 {
    return
  }
  fromDir := filepath.Dir(fromFile)

  weight := func(s Symbol) int {
    switch {
    case s.File == fromFile:
      return 0
    case filepath.Dir(s.File) == fromDir:
      return 1
    default:
      return 2
    }
  }

  // Insertion sort: these lists are small (rarely more than a
  // handful of same-named symbols) and this keeps equal-weight
  // matches in their original relative order, which sort.Slice does
  // not guarantee. current must be saved before the shift loop
  // runs — matches[i] gets overwritten by the first shift
  // otherwise, losing the element.
  for i := 1; i < len(matches); i++ {
    current := matches[i]
    w := weight(current)
    j := i - 1
    for j >= 0 && weight(matches[j]) > w {
      matches[j + 1] = matches[j]
      j--
    }
    matches[j + 1] = current
  }
}

// kindLabels maps common ctags kind letters to a human label for
// prompts. Kind letters are language-specific; unmapped ones just
// show as-is rather than erroring.
var kindLabels = map[string]string{
  "f": "function", "m": "method/member", "s": "struct", "c": "class",
  "v": "variable", "p": "package", "i": "interface", "t": "type",
  "e": "enumerator", "g": "enum", "d": "macro", "n": "namespace",
  "l": "local", "u": "union", "C": "constant",
}

// KindLabel returns a human-readable label for a ctags kind letter,
// falling back to the raw letter if it's not in the (necessarily
// incomplete, language-specific) map above.
func KindLabel(kind string) string {
  if label, ok := kindLabels[kind]; ok {
    return label
  }
  return kind
}
