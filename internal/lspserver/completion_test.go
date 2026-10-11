package lspserver

import "testing"

func TestSplitAtPosition(t *testing.T) {
  text := "func main() {\n\tfmt.Println()\n}\n"

  prefix, suffix, ok := splitAtPosition(text, 1, 5)
  if !ok {
    t.Fatal("splitAtPosition returned ok=false")
  }
  if prefix != "func main() {\n\tfmt." {
    t.Fatalf("prefix = %q", prefix)
  }
  if suffix != "Println()\n}\n" {
    t.Fatalf("suffix = %q", suffix)
  }
  if prefix + suffix != text {
    t.Fatalf("prefix+suffix lost or duplicated bytes:\n%q\n+\n%q\n!=\n%q", prefix, suffix, text)
  }
}

func TestSplitAtPositionClampsCharacter(t *testing.T) {
  text := "abc\ndef\n"

  _, suffix, ok := splitAtPosition(text, 0, 999)
  if !ok || suffix != "\ndef\n" {
    t.Fatalf("character past end of line: suffix = %q, ok = %v", suffix, ok)
  }

  prefix, _, ok := splitAtPosition(text, 0, -5)
  if !ok || prefix != "" {
    t.Fatalf("negative character: prefix = %q, ok = %v", prefix, ok)
  }
}

func TestSplitAtPositionOutOfRangeLine(t *testing.T) {
  if _, _, ok := splitAtPosition("only one line", 5, 0); ok {
    t.Fatal("expected ok=false for a line past the end of the document")
  }
}

func TestLastLines(t *testing.T) {
  text := "1\n2\n3\n4\n5"
  if got := lastLines(text, 2); got != "4\n5" {
    t.Fatalf("lastLines(_, 2) = %q, want \"4\\n5\"", got)
  }
  if got := lastLines(text, 100); got != text {
    t.Fatalf("lastLines with n > line count should return the input unchanged, got %q", got)
  }
  if got := lastLines(text, 0); got != "" {
    t.Fatalf("lastLines(_, 0) = %q, want \"\" (zero lines requested)", got)
  }
  if got := lastLines(text, 1); got != "5" {
    t.Fatalf("lastLines(_, 1) = %q, want just the final line", got)
  }
}

func TestFirstLines(t *testing.T) {
  text := "1\n2\n3\n4\n5"
  if got := firstLines(text, 2); got != "1\n2" {
    t.Fatalf("firstLines(_, 2) = %q, want \"1\\n2\"", got)
  }
  if got := firstLines(text, 100); got != text {
    t.Fatalf("firstLines with n > line count should return the input unchanged, got %q", got)
  }
}

func TestCleanCompletion(t *testing.T) {
  cases := []struct {
    name string
    raw  string
    want string
  }{
    {"plain code", "return x + 1", "return x + 1"},
    {
      "fenced with language tag",
      "```go\nreturn x + 1\n```",
      "return x + 1",
    },
    {"trailing blank lines trimmed", "return x\n\n\n", "return x"},
    {"empty input", "", ""},
    {"whitespace only", "   \n  ", ""},
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      if got := cleanCompletion(c.raw, 20); got != c.want {
        t.Fatalf("cleanCompletion(%q) = %q, want %q", c.raw, got, c.want)
      }
    })
  }
}

func TestCleanCompletionCapsLineCount(t *testing.T) {
  raw := "1\n2\n3\n4\n5\n6\n7\n8\n9\n10"
  got := cleanCompletion(raw, 3)
  if got != "1\n2\n3" {
    t.Fatalf("cleanCompletion with maxLines=3 = %q, want \"1\\n2\\n3\"", got)
  }
}

// TestSplitAtPositionAllocationsStayBounded: the old implementation
// strings.Split the whole document, sliced it, then strings.Join'd two
// pieces back together — three full passes doing real allocation work
// on every single inline-completion request, which fires on a debounce
// timer while the user is still typing. The rewrite should cost O(1)
// allocations (two string headers out) regardless of document size.
func TestSplitAtPositionAllocationsStayBounded(t *testing.T) {
  var b []byte
  for i := 0; i < 20000; i++ {
    b = append(b, []byte("some line of source code here\n")...)
  }
  big := string(b)

  const allocLimit = 4
  allocs := testing.AllocsPerRun(20, func() {
    splitAtPosition(big, 10000, 10)
  })
  if allocs > allocLimit {
    t.Fatalf(
      "splitAtPosition on a 20000-line document allocated %.0f times, want <= %d",
      allocs, allocLimit,
    )
  }
}
