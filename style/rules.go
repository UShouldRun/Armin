// Reading rules.md: every ### heading starts a rule, and its "- key:
// value" lines set the rule's conditions; prose around them is for
// the reader.
//
//   loadRules   parse the file into rules
//   field       one "- key: value" line onto a rule
//   value       a value without its backquotes
//   pick        the rule with a name, for -only

package main

import (
  "bufio"
  "fmt"
  "os"
  "regexp"
  "strconv"
  "strings"
)

// loadRules reads the rules file at path.
func loadRules(path string) ([]*rule, error) {
  f, err := os.Open(path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in loadRules:\n\t%w", err)
  }

  defer f.Close()

  var rules []*rule
  var cur *rule
  sc := bufio.NewScanner(f)
  n := 0
  for sc.Scan() {
    n++
    line := sc.Text()
    switch {
    case strings.HasPrefix(line, "### "):
      cur = &rule{ name: strings.TrimSpace(line[4:]) }
      rules = append(rules, cur)
    case strings.HasPrefix(line, "## "):
      cur = nil
    case cur != nil && strings.HasPrefix(line, "- "):
      err = field(cur, line[2:])
      if err != nil {
        return nil, fmt.Errorf(
          "\n\t... in loadRules:\n\t %s:%d: %w", path, n, err,
        )
      }
    }
  }

  return rules, sc.Err()
}

// field sets one condition on r from "key: value".
func field(r *rule, kv string) error {
  key, raw, ok := strings.Cut(kv, ":")
  if !ok {
    return fmt.Errorf("want \"key: value\", got %q", kv)
  }

  v := value(raw)
  var err error
  switch strings.TrimSpace(key) {
  case "files":
    r.files = list(v)
  case "skip-files":
    r.skip = list(v)
  case "on":
    r.onLine = v == "line"
  case "match":
    r.match, err = regexp.Compile(v)
  case "except":
    r.except, err = regexp.Compile(v)
  case "next":
    r.next, err = regexp.Compile(v)
  case "next-not":
    r.nextNot, err = regexp.Compile(v)
  case "opener":
    r.opener, err = regexp.Compile(v)
  case "next-blank":
    r.nextNotBlank = v == "no"
  case "prev-blank":
    r.prevNotBlank = v == "no"
  case "same-indent":
    r.sameIndent = v == "yes"
  case "unclosed":
    r.unclosed, err = strconv.Atoi(v)
  default:
    return fmt.Errorf("unknown key %q", key)
  }

  return err
}

// value strips a value's backquotes: `x`, or `` x`y `` for one that
// holds a backquote itself.
func value(raw string) string {
  v := strings.TrimSpace(raw)
  if strings.HasPrefix(v, "``") && strings.HasSuffix(v, "``") {
    return strings.TrimSpace(v[2:len(v) - 2])
  }

  if strings.HasPrefix(v, "`") && strings.HasSuffix(v, "`") {
    return v[1:len(v) - 1]
  }

  return v
}

// list splits a comma-separated value.
func list(v string) []string {
  var out []string
  for _, s := range strings.Split(v, ",") {
    s = strings.TrimSpace(value(s))
    if s != "" {
      out = append(out, s)
    }
  }

  return out
}

// pick keeps the rule named name.
func pick(rules []*rule, name string) []*rule {
  for _, r := range rules {
    if r.name == name {
      return []*rule{ r }
    }
  }

  return nil
}
