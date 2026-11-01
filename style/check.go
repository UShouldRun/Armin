// Checking: every file under the roots that some rule covers, every
// line against every rule that covers the file.
//
//   checkTree   walk the roots, check each file, count breaks by rule
//   checkFile   one file's lines against the rules
//   breaks      whether one line breaks one rule
//   report      the count per rule, and the total

package main

import (
  "fmt"
  "io"
  "io/fs"
  "os"
  "path/filepath"
  "strings"
)

// skipDirs are never walked into.
var skipDirs = map[string]bool{
  ".git": true, "node_modules": true, "vendor": true, "testdata": true,
}

// checkTree checks every covered file under roots, printing each
// break to w, and returns the breaks per rule.
func checkTree(
  roots []string, rules []*rule, w io.Writer,
) (map[*rule]int, error) {
  counts := map[*rule]int{}
  for _, root := range roots {
    visit := func(p string, d fs.DirEntry, err error) error {
      if err != nil {
        return err
      }

      if d.IsDir() {
        if skipDirs[d.Name()] {
          return filepath.SkipDir
        }

        return nil
      }

      return checkFile(p, rules, counts, w)
    }

    err := filepath.WalkDir(root, visit)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in checkTree:\n\t%w", err)
    }
  }

  return counts, nil
}

// checkFile checks one file against the rules covering it.
func checkFile(
  path string, rules []*rule, counts map[*rule]int, w io.Writer,
) error {
  var mine []*rule
  for _, r := range rules {
    if globs(r.files, path) && !globs(r.skip, path) {
      mine = append(mine, r)
    }
  }

  if len(mine) == 0 {
    return nil
  }

  src, err := os.ReadFile(path)
  if err != nil {
    return fmt.Errorf("\n\t... in checkFile:\n\t%w", err)
  }

  lines := lexLines(string(src), strings.HasSuffix(path, ".js"))
  for i := range lines {
    for _, r := range mine {
      if breaks(r, lines, i) {
        counts[r]++
        fmt.Fprintf(w, "%s:%d: %s\n", path, i + 1, r.name)
      }
    }
  }

  return nil
}

// breaks says whether line i breaks r.
func breaks(r *rule, lines []srcLine, i int) bool {
  l := lines[i]
  if l.inText {
    return false
  }

  seen := l.code
  if r.onLine {
    seen = l.text
  }

  if r.match != nil && !r.match.MatchString(seen) {
    return false
  }

  if r.except != nil && r.except.MatchString(l.text) {
    return false
  }

  if r.unclosed > 0 && open(l.code) < r.unclosed {
    return false
  }

  if r.opener != nil &&
    (l.opener < 0 || !r.opener.MatchString(lines[l.opener].code)) {
    return false
  }

  if r.nextNotBlank &&
    (i + 1 >= len(lines) || strings.TrimSpace(lines[i + 1].text) == "") {
    return false
  }

  if r.prevNotBlank && (i == 0 || strings.TrimSpace(lines[i - 1].text) == "") {
    return false
  }

  next := i + 1
  for next < len(lines) && strings.TrimSpace(lines[next].text) == "" {
    next++
  }

  if r.next != nil || r.nextNot != nil || r.sameIndent {
    if next >= len(lines) {
      return false
    }

    n := lines[next].code
    if r.next != nil && !r.next.MatchString(n) {
      return false
    }

    if r.nextNot != nil && r.nextNot.MatchString(n) {
      return false
    }

    if r.sameIndent && indent(n) != indent(l.code) {
      return false
    }
  }

  return true
}

// open counts the brackets a line leaves open: those it opens that
// aren't closed later on it.
func open(code string) int {
  depth, low := 0, 0
  for i := 0; i < len(code); i++ {
    switch code[i] {
    case '(', '[', '{':
      depth++
    case ')', ']', '}':
      depth--
      low = min(low, depth)
    }
  }

  return depth - low
}

func indent(s string) int {
  return len(s) - len(strings.TrimLeft(s, " \t"))
}

// globs says whether path's base name matches any of patterns.
func globs(patterns []string, path string) bool {
  base := filepath.Base(path)
  for _, p := range patterns {
    ok, _ := filepath.Match(p, base)
    if ok {
      return true
    }
  }

  return false
}

// report prints the breaks per rule, in the file's order, and returns
// the total.
func report(rules []*rule, counts map[*rule]int, w io.Writer) int {
  total := 0
  for _, r := range rules {
    if counts[r] > 0 {
      fmt.Fprintf(w, "%5d  %s\n", counts[r], r.name)
      total += counts[r]
    }
  }

  if total == 0 {
    fmt.Fprintln(w, "clean")
  }

  return total
}
