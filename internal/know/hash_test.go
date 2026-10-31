package know

import (
  "strings"
  "testing"
)

func TestRollFindsMovedLines(t *testing.T) {
  orig := "a\nb\nfunc f() {\n  return 1\n}\nc\n"
  lines := LineHashes([]byte(orig), nil)
  want := RangeHash(lines[2:5])

  moved := "x\ny\nz\na\nb\nfunc f() {\n  return 1\n}\nc\n"
  got := Roll(LineHashes([]byte(moved), nil), 3, want, 2)
  if got != 5 {
    t.Fatalf("moved range found at %d, want 5", got)
  }

  changed := strings.Replace(moved, "return 1", "return 2", 1)
  if Roll(LineHashes([]byte(changed), nil), 3, want, 2) != -1 {
    t.Fatal("a changed range should not be found")
  }

  // the same code twice: the copy nearest the old place wins
  twice := "func f() {\n  return 1\n}\n" + strings.Repeat("pad\n", 10) +
    "func f() {\n  return 1\n}\n"
  hs := LineHashes([]byte(twice), nil)
  if Roll(hs, 3, want, 12) != 13 || Roll(hs, 3, want, 1) != 0 {
    t.Fatal("nearest match not preferred")
  }
}

func TestOneByteEditChangesTheHash(t *testing.T) {
  a := LineHashes([]byte("alpha\nbeta\ngamma\n"), nil)
  b := LineHashes([]byte("alpha\nbetb\ngamma\n"), nil)
  if RangeHash(a) == RangeHash(b) {
    t.Fatal("a one-byte edit kept the hash")
  }
}
