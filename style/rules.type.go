// The types of rules.go.

package main

import "regexp"

// rule is one ### section of rules.md: a name, and the conditions a
// line has to meet to break it.
type rule struct {
  name        string
  files, skip []string

  match, except, next, nextNot, opener *regexp.Regexp

  onLine, nextNotBlank, prevNotBlank, sameIndent bool
  unclosed                                       int
}
