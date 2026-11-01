package ui

import (
  "strings"
  "testing"
)

func TestHighlight(t *testing.T) {
  was := Enabled
  Enabled = true
  defer func() { Enabled = was }()

  src := []string{
    "/* a comment",
    "   over two lines */",
    "func f() string {",
    `  return "hi" // done`,
    "}",
  }

  got := Highlight("x.go", src)
  if len(got) != len(src) {
    t.Fatalf("%d lines, want %d", len(got), len(src))
  }

  for i, want := range []string{
    synComment + "/* a comment" + reset,
    synComment + "   over two lines */" + reset,
  } {
    if got[i] != want {
      t.Errorf("line %d = %q, want %q", i + 1, got[i], want)
    }
  }

  if !strings.Contains(got[2], synFuncKw + "func" + reset) ||
    !strings.Contains(got[2], synFunc + "f" + reset) ||
    !strings.Contains(got[2], synNumber + "string" + reset) {
    t.Errorf("line 3 = %q", got[2])
  }

  if !strings.Contains(got[3], synFuncKw + "return" + reset) ||
    !strings.Contains(got[3], synString + `"hi"` + reset) ||
    !strings.Contains(got[3], synComment + "// done" + reset) {
    t.Errorf("line 4 = %q", got[3])
  }

  calls := Highlight("x.go", []string{ `fmt.Println("a\n", true, nil)` })[0]
  if !strings.Contains(calls, synFunc + "Println" + reset) ||
    !strings.Contains(calls, synFuncKw + `\n` + reset) ||
    !strings.Contains(calls, synBool + "true" + reset) ||
    !strings.Contains(calls, synFuncKw + "nil" + reset) {
    t.Errorf("calls = %q", calls)
  }

  plain := Highlight("notes.txt", src)
  if plain[2] != src[2] {
    t.Error("an unknown language should come back plain")
  }

  Enabled = false
  if Highlight("x.go", src)[2] != src[2] {
    t.Error("no color, no codes")
  }
}
