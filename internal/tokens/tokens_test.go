package tokens

import (
  "strings"
  "testing"
)

func TestTerms(t *testing.T) {
  got := Query(
    "The sync.WaitGroup waits for Goroutines; " +
      "1234567 x ÉCOLE threads",
  )
  want := "sync waitgroup wait goroutine école thread"
  if strings.Join(got, " ") != want {
    t.Fatalf("Query = %q, want %q", strings.Join(got, " "), want)
  }

  buf := make([]byte, 0, 64)
  text := "Share memory by communicating; don't communicate by " +
    "sharing memory."
  allocs := testing.AllocsPerRun(
    100,
    func() {
      Each(text, buf, func([]byte) {})
    },
  )

  if allocs != 0 {
    t.Fatalf("Each allocates %.0f times per call", allocs)
  }
}
