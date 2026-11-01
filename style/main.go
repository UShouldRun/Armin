// Command style checks a source tree against rules.md: each rule a
// regular expression and a few conditions, explained in prose next to
// it. Run from the repository root: go -C style run . ..
package main

import (
  "flag"
  "fmt"
  "os"
)

func main() {
  err := run()
  if err != nil {
    fmt.Fprintln(os.Stderr, "style:", err)
    os.Exit(2)
  }
}

// run reads the rules, walks the roots, prints every break and a
// count per rule, and exits 1 when anything broke.
func run() error {
  rulesPath := flag.String("rules", "rules.md", "the rules file")
  only := flag.String("only", "", "check only the rule with this name")
  flag.Parse()

  rules, err := loadRules(*rulesPath)
  if err != nil {
    return err
  }

  if *only != "" {
    rules = pick(rules, *only)
  }

  roots := flag.Args()
  if len(roots) == 0 {
    roots = []string{ ".." }
  }

  counts, err := checkTree(roots, rules, os.Stdout)
  if err != nil {
    return err
  }

  if report(rules, counts, os.Stdout) > 0 {
    os.Exit(1)
  }

  return nil
}
