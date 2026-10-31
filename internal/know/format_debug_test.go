package know

import (
  "fmt"
  "os"
  "strconv"
  "strings"
  "testing"
)

// TestFormatDebug prints a question with both formats' lines, to look
// at a miss offline: ARMIN_FORMAT_DEBUG=hard:22 (or easy:12).
func TestFormatDebug(t *testing.T) {
  spec := os.Getenv("ARMIN_FORMAT_DEBUG")
  if spec == "" {
    t.Skip()
  }

  set, n, _ := strings.Cut(spec, ":")
  i, _ := strconv.Atoi(n)
  g, all := evalTree()
  qs := evalQuestions(g, all)
  if set == "hard" {
    qs = evalHardQuestions(g, all)
  }

  q := qs[i - 1]
  fmt.Printf("Q: %s\nwant: %s\n\n", q.text, q.want)
  for _, id := range q.shown {
    var a, b strings.Builder
    g.Line(&a, id, false)
    codedLine(g, &b, id)
    fmt.Printf("%s\n%s\n\n", a.String(), b.String())
  }
}
