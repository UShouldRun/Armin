// Package repomap builds a static map of a project for the system
// prompt: the baseline the knowledge DAG is measured against. It is
// aider's repo map without tree-sitter or a graph: the definitions
// ctags finds, each file ranked by how often the rest of the project
// names what it defines, cut to a byte budget. Static, rebuilt per
// session, nothing remembered.
//
//   Build    the map: definitions, references, ranking, rendering
//   refs     how often each defined name appears outside its file
//   render   the ranked files and their best definitions, in budget

package repomap

import (
  "context"
  "fmt"
  "math"
  "os"
  "path/filepath"
  "slices"
  "strings"

  "armin/internal/ctags"
)

const (
  // perFile caps the definitions listed under one file.
  perFile = 4

  // shortName is the longest name too common to count as a
  // reference ("id", "ok", "n").
  shortName = 2
)

// kinds are the ctags kind letters worth a line in the map: what code
// calls or names (functions, types, structs, interfaces, classes,
// constants, aliases, macros, enums), not variables, fields or
// packages.
var kinds = map[string]bool{
  "f": true, "t": true, "s": true, "i": true, "c": true, "a": true,
  "d": true, "g": true, "C": true,
}

// mapped says whether s gets a line: "m" is a method in Python and
// JavaScript but a struct field in Go and C.
func mapped(s ctags.Symbol) bool {
  if strings.HasSuffix(s.File, "_test.go") {
    return false
  }

  if s.Kind == "m" {
    ext := filepath.Ext(s.File)
    return ext == ".py" || ext == ".js" || ext == ".ts"
  }

  return kinds[s.Kind]
}

// Build is the project's map in at most budget bytes, or "" when
// ctags finds nothing.
func Build(ctx context.Context, idx *ctags.Index, budget int) string {
  var defs []def
  for _, s := range idx.Search(ctx, "") {
    if !mapped(s) {
      continue
    }

    defs = append(defs, def{ sym: s })
  }

  if len(defs) == 0 {
    return ""
  }

  refs(idx.Root(), defs)

  return render(defs, budget)
}

// refs sets each definition's score: how many times its name appears
// in the project's other files that can see it, split between the
// files defining the same name (every package has a New).
func refs(root string, defs []def) {
  byName := map[string][]int{}
  files := map[string]bool{}
  for i, d := range defs {
    if len(d.sym.Name) > shortName {
      byName[d.sym.Name] = append(byName[d.sym.Name], i)
    }

    files[d.sym.File] = true
  }

  counts := make([]float64, len(defs))
  for file := range files {
    data, err := os.ReadFile(filepath.Join(root, file))
    if err != nil {
      continue
    }

    eachIdent(data, func(name string) {
      for _, i := range byName[name] {
        if reaches(defs[i].sym, file) {
          counts[i] += 1 / float64(len(byName[name]))
        }
      }
    })
  }

  for i := range defs {
    defs[i].score = counts[i]
  }
}

// reaches says whether file can name s: any other file, except that
// an unexported Go name is only visible in its own package (its
// directory), so a local variable elsewhere that happens to share the
// name doesn't count.
func reaches(s ctags.Symbol, file string) bool {
  if file == s.File {
    return false
  }

  c := s.Name[0]
  if filepath.Ext(s.File) == ".go" && !(c >= 'A' && c <= 'Z') {
    return filepath.Dir(file) == filepath.Dir(s.File)
  }

  return true
}

// eachIdent calls emit with every identifier in src: a run of ASCII
// letters, digits and "_" starting with a letter or "_".
func eachIdent(src []byte, emit func(string)) {
  start := -1
  for i := 0; i <= len(src); i++ {
    word := i < len(src) && identByte(src[i])
    switch {
    case word && start < 0:
      start = i
    case !word && start >= 0:
      if src[start] < '0' || src[start] > '9' {
        emit(string(src[start:i]))
      }

      start = -1
    }
  }
}

func identByte(c byte) bool {
  return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
    c >= '0' && c <= '9' || c == '_'
}

// render lists files by rank, the sum of their definitions' damped
// scores (one name used everywhere shouldn't carry a file alone), each
// with its best definitions, until the budget is spent. Ties fall to
// the path, so the same project gives the same bytes.
func render(defs []def, budget int) string {
  byFile := map[string][]def{}
  rank := map[string]float64{}
  for _, d := range defs {
    byFile[d.sym.File] = append(byFile[d.sym.File], d)
    rank[d.sym.File] += math.Sqrt(d.score)
  }

  var files []string
  for f := range byFile {
    files = append(files, f)
  }

  slices.SortFunc(files, func(a, b string) int {
    if rank[a] != rank[b] {
      if rank[a] > rank[b] {
        return -1
      }

      return 1
    }

    return strings.Compare(a, b)
  })

  var b strings.Builder
  for _, f := range files {
    ds := byFile[f]
    slices.SortStableFunc(ds, func(x, y def) int {
      if x.score != y.score {
        if x.score > y.score {
          return -1
        }

        return 1
      }

      return x.sym.Line - y.sym.Line
    })

    var names []string
    for _, d := range ds[:min(perFile, len(ds))] {
      names = append(names, fmt.Sprintf("%s %d", label(d.sym), d.sym.Line))
    }

    entry := f + "\n  " + strings.Join(names, " · ") + "\n"
    if b.Len() + len(entry) > budget {
      break
    }

    b.WriteString(entry)
  }

  return b.String()
}

// label is a definition's name, a method's with its type ("Loop.Turn"):
// ctags scopes a method to its struct or class, or to "receiver:" when
// it couldn't resolve the type.
func label(s ctags.Symbol) string {
  kind, owner, ok := strings.Cut(s.Scope, ":")
  if !ok || kind != "struct" && kind != "class" && kind != "receiver" {
    return s.Name
  }

  i := strings.LastIndexByte(owner, '.')

  return owner[i + 1:] + "." + s.Name
}
