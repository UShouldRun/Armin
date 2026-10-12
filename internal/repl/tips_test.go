package repl

import (
  "testing"
  "time"
)

func TestTipAt(t *testing.T) {
  tips := []string{ "a", "b", "c" }

  got := tipAt(tips, 0, time.Second)
  if got != "" {
    t.Fatalf("tip before tipAfter = %q, want none", got)
  }

  got = tipAt(tips, 1, tipAfter)
  if got != "b" {
    t.Fatalf("first tip = %q, want b", got)
  }

  got = tipAt(tips, 1, tipAfter + 2 * tipEvery)
  if got != "a" {
    t.Fatalf("rotated tip = %q, want a", got)
  }

  got = tipAt(nil, 0, time.Hour)
  if got != "" {
    t.Fatalf("no tips = %q", got)
  }
}
