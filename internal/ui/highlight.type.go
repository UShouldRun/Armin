// The types of highlight.go.

package ui

// lang is one language family for the highlighter: its comment and
// string syntax and its word lists.
type lang struct {
  line              string    // line comment start
  block             [2]string // block comment delimiters, or empty
  quote             string    // characters that open a string
  kw, lit, types    map[string]bool
  fold              bool // case-insensitive words (SQL)
}
