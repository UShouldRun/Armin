package lspserver

import "testing"

func TestWordAt(t *testing.T) {
  cases := []struct {
    name      string
    line      string
    character int
    want      string
  }{
    {"middle of word", "fooBar(x)", 2, "fooBar"},
    {"cursor on last letter of word", "fooBar(x)", 5, "fooBar"},
    {"cursor at start of word", "fooBar(x)", 0, "fooBar"},
    {"cursor on whitespace", "foo bar", 3, ""},
    {"cursor on punctuation", "foo(bar)", 3, ""},
    {"underscore identifier", "my_var_2 = 1", 4, "my_var_2"},
    {"empty line", "", 0, ""},
    {"character past end of line", "foo", 99, ""},
    {"negative character", "foo", -5, ""},
  }

  for _, c := range cases {
    t.Run(c.name, func(t *testing.T) {
      if got := wordAt(c.line, c.character); got != c.want {
        t.Fatalf("wordAt(%q, %d) = %q, want %q", c.line, c.character, got, c.want)
      }
    })
  }
}

func TestSymbolKindFor(t *testing.T) {
  if got := symbolKindFor("f"); got != 12 {
    t.Fatalf("symbolKindFor(f) = %d, want 12 (Function)", got)
  }
  if got := symbolKindFor("nonsense-kind"); got != 13 {
    t.Fatalf("symbolKindFor(unknown) = %d, want 13 (Variable fallback)", got)
  }
}

func TestPointRange(t *testing.T) {
  r := pointRange(5)
  if r.Start.Line != 4 || r.End.Line != 4 {
    t.Fatalf("pointRange(5) = %+v, want 0-indexed line 4", r)
  }
  if r.Start.Character != 0 || r.End.Character != 0 {
    t.Fatalf("pointRange should be zero-width at column 0, got %+v", r)
  }

  // ctags line numbers are 1-indexed; a (defensively handled) 0 or
  // negative input must not go negative in the 0-indexed LSP range.
  if got := pointRange(0); got.Start.Line != 0 {
    t.Fatalf("pointRange(0) = %+v, want clamped to line 0", got)
  }
}
