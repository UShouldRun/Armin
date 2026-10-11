package tools

import (
  "context"
  "fmt"
  "go/ast"
  "go/parser"
  "go/token"
  "io/fs"
  "path/filepath"
  "strconv"
  "strings"
  "sync"
  "time"
  "unicode"

  "armin/internal/ctags"
  "armin/internal/sandbox"
)

// CallSite is one place a function/method name is invoked.
type CallSite struct {
  File string `json:"file"`
  Line int    `json:"line"`
}

// callGraphData is one full-tree AST scan's result: byName maps a
// called identifier to every call site, enclosing maps a call site
// back to the name of the top-level function/method it's inside —
// built together in one pass (scanCalls) so impact_analysis's BFS
// doesn't need a second walk to step outward.
type callGraphData struct {
  byName    map[string][]CallSite
  enclosing map[string]string // "file:line" -> enclosing function name
}

func callSiteKey(c CallSite) string {
  return c.File + ":" + strconv.Itoa(c.Line)
}

// scanCalls parses every .go file under root with go/parser (syntax
// only — armin has no golang.org/x/tools/go/packages dependency and
// isn't taking one on for this) and records every call expression
// inside a top-level function/method body by the called function's
// bare identifier name. This is purely syntactic: a method call
// x.Foo() matches by the name "Foo" regardless of x's type, so a
// common short name (Close, Run, ...) aliases unrelated methods
// across the codebase — every tool built on this says so. Calls at
// package scope (var initializers, closures not inside a named
// func) aren't tracked.
func scanCalls(root string) (*callGraphData, error) {
  data := &callGraphData{
    byName: map[string][]CallSite{}, enclosing: map[string]string{},
  }
  fset := token.NewFileSet()

  err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
    if err != nil {
      return err
    }
    if d.IsDir() {
      if d.Name() != "." && skipDirs[d.Name()] {
        return filepath.SkipDir
      }
      return nil
    }
    if !strings.HasSuffix(p, ".go") {
      return nil
    }

    file, perr := parser.ParseFile(fset, p, nil, 0)
    if perr != nil {
      return nil // a file that doesn't parse is skipped, not fatal to the whole scan
    }

    rel, relErr := filepath.Rel(root, p)
    if relErr != nil {
      rel = p
    }
    rel = filepath.ToSlash(rel)

    for _, decl := range file.Decls {
      fn, ok := decl.(*ast.FuncDecl)
      if !ok {
        continue
      }
      fnName := fn.Name.Name

      ast.Inspect(fn, func(n ast.Node) bool {
        call, ok := n.(*ast.CallExpr)
        if !ok {
          return true
        }
        name := calleeName(call.Fun)
        if name == "" {
          return true
        }
        site := CallSite{ File: rel, Line: fset.Position(call.Pos()).Line }
        data.byName[name] = append(data.byName[name], site)
        data.enclosing[callSiteKey(site)] = fnName
        return true
      })
    }
    return nil
  })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in scanCalls:\n\t%w", err)
  }
  return data, nil
}

// calleeName extracts the bare identifier a call expression invokes
// — "Foo" from both Foo(...) and x.Foo(...) — or "" for calls this
// syntactic pass doesn't handle (a call through a function value,
// for instance).
func calleeName(fun ast.Expr) string {
  switch f := fun.(type) {
  case *ast.Ident:
    return f.Name
  case *ast.SelectorExpr:
    return f.Sel.Name
  default:
    return ""
  }
}

// callGraphCache mirrors ctags.Index's short-TTL pattern: parsing
// every .go file is real work, and find_callers/find_dead_code/
// impact_analysis can all be called multiple times in one
// conversation turn.
type callGraphCache struct {
  mu      sync.Mutex
  root    string
  data    *callGraphData
  builtAt time.Time
}

const callGraphStaleAfter = 5 * time.Second

var (
  callGraphCachesMu sync.Mutex
  callGraphCaches   = map[string]*callGraphCache{}
)

func getCallGraphCache(root string) *callGraphCache {
  callGraphCachesMu.Lock()
  defer callGraphCachesMu.Unlock()

  c, ok := callGraphCaches[root]
  if !ok {
    c = &callGraphCache{ root: root }
    callGraphCaches[root] = c
  }
  return c
}

func (c *callGraphCache) get() (*callGraphData, error) {
  c.mu.Lock()
  defer c.mu.Unlock()

  if c.data != nil && time.Since(c.builtAt) < callGraphStaleAfter {
    return c.data, nil
  }

  data, err := scanCalls(c.root)
  if err != nil {
    return nil, err
  }
  c.data, c.builtAt = data, time.Now()
  return data, nil
}

// --- find_callers ---

type FindCallersInput struct {
  Name string `json:"name"`
}

type FindCallersOutput struct {
  Calls []CallSite `json:"calls"`
}

// FindCallers is syntactic, not type-checked (see scanCalls): it
// matches Name against every call site's identifier text, so
// x.Foo() and y.Foo() on unrelated types both match "Foo" — treat
// results as leads, not certainties.
func FindCallers(
  ctx context.Context, cfg *sandbox.Config, in FindCallersInput,
) (*FindCallersOutput, error) {
  if in.Name == "" {
    return nil, fmt.Errorf("\n\t... in FindCallers:\n\t name is required")
  }
  data, err := getCallGraphCache(cfg.Root).get()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FindCallers:\n\t%w", err)
  }
  return &FindCallersOutput{ Calls: data.byName[in.Name] }, nil
}

// --- find_dead_code ---

type FindDeadCodeInput struct {
  Path string `json:"path,omitempty"` // limit the exported-symbol scan to this file/dir prefix
}

type DeadCodeCandidate struct {
  Name string `json:"name"`
  File string `json:"file"`
  Line int    `json:"line"`
}

type FindDeadCodeOutput struct {
  Candidates []DeadCodeCandidate `json:"candidates"`
}

// FindDeadCode cross-references ctags' exported Go symbols against
// scanCalls' syntactic call sites: an exported function/method with
// zero matching call sites anywhere in the indexed tree is flagged
// "possibly unused." This can false-positive on reflection-based
// use, an external module consuming this package's public API, or
// any call scanCalls' syntax-only pass doesn't recognize — verify
// before deleting anything it flags.
//
// One confirmed, common false-positive source: Universal Ctags
// tags both Go struct fields and methods with the same kind letter
// ("m"), and ctags.Symbol doesn't currently carry enough (a
// signature or typeref) to tell them apart. A struct field is never
// "called," so on a struct-heavy codebase expect a real chunk of
// the results to be exported fields, not dead functions — read the
// File/Line before trusting a candidate.
func FindDeadCode(
  ctx context.Context, idx *ctags.Index, cfg *sandbox.Config,
  in FindDeadCodeInput,
) (*FindDeadCodeOutput, error) {
  if idx == nil {
    return nil, ctagsUnavailable("FindDeadCode")
  }

  data, err := getCallGraphCache(cfg.Root).get()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in FindDeadCode:\n\t%w", err)
  }

  out := &FindDeadCodeOutput{}
  for _, sym := range idx.Search(ctx, "") {
    if sym.Kind != "f" && sym.Kind != "m" {
      continue // only functions/methods are meaningfully "dead code" here
    }
    if !isExportedGo(sym.Name) {
      continue
    }
    if in.Path != "" && !strings.HasPrefix(sym.File, in.Path) {
      continue
    }
    if len(data.byName[sym.Name]) > 0 {
      continue // has at least one syntactic call site
    }
    out.Candidates = append(out.Candidates, DeadCodeCandidate{
      Name: sym.Name, File: sym.File, Line: sym.Line,
    })
  }
  return out, nil
}

func isExportedGo(name string) bool {
  r := []rune(name)
  return len(r) > 0 && unicode.IsUpper(r[0])
}

// --- impact_analysis ---

type ImpactAnalysisInput struct {
  Name  string `json:"name"`
  Depth int    `json:"depth,omitempty"` // default 2, max 3
}

type ImpactAnalysisOutput struct {
  Affected  []string `json:"affected"` // function names that transitively call Name
  Truncated bool     `json:"truncated,omitempty"`
}

const (
  defaultImpactDepth = 2
  maxImpactDepth     = 3
  maxImpactResults   = 100
)

// ImpactAnalysis BFS's outward from Name over the same syntactic
// call graph find_callers uses (same caveats apply — see scanCalls)
// to Depth levels of "who calls a caller of this," bounded so a
// highly-connected symbol doesn't return the whole codebase.
func ImpactAnalysis(
  ctx context.Context, cfg *sandbox.Config, in ImpactAnalysisInput,
) (*ImpactAnalysisOutput, error) {
  if in.Name == "" {
    return nil, fmt.Errorf("\n\t... in ImpactAnalysis:\n\t name is required")
  }
  depth := in.Depth
  if depth <= 0 {
    depth = defaultImpactDepth
  }
  if depth > maxImpactDepth {
    depth = maxImpactDepth
  }

  data, err := getCallGraphCache(cfg.Root).get()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ImpactAnalysis:\n\t%w", err)
  }

  visited := map[string]bool{ in.Name: true }
  frontier := []string{ in.Name }
  var affected []string
  truncated := false

  for d := 0; d < depth && len(frontier) > 0; d++ {
    var next []string
    for _, name := range frontier {
      for _, site := range data.byName[name] {
        caller, ok := data.enclosing[callSiteKey(site)]
        if !ok || visited[caller] {
          continue
        }
        visited[caller] = true
        if len(affected) >= maxImpactResults {
          truncated = true
          continue
        }
        affected = append(affected, caller)
        next = append(next, caller)
      }
    }
    frontier = next
  }

  return &ImpactAnalysisOutput{ Affected: affected, Truncated: truncated }, nil
}
