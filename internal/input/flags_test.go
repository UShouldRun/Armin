package input

import (
  "strings"
  "reflect"
  "testing"
)

func TestExtractPositionalRootVariousOrderings(t *testing.T) {
  cases := []struct {
    name         string
    args         []string
    wantRest     []string
    wantPositional string
  }{
    {
      name: "no args",
      args: nil, wantRest: []string{}, wantPositional: "",
    },
    {
      name: "bare positional only",
      args: []string{"."}, wantRest: []string{}, wantPositional: ".",
    },
    {
      name: "positional first",
      args:           []string{".", "-mcp", "-no-color"},
      wantRest:       []string{"-mcp", "-no-color"},
      wantPositional: ".",
    },
    {
      name:           "positional in the middle",
      args:           []string{"-mcp", ".", "-no-color"},
      wantRest:       []string{"-mcp", "-no-color"},
      wantPositional: ".",
    },
    {
      name:           "positional last",
      args:           []string{"-mcp", "-no-color", "."},
      wantRest:       []string{"-mcp", "-no-color"},
      wantPositional: ".",
    },
    {
      name:           "value-taking flag keeps its value attached",
      args:           []string{"-model", "foo", ".", "-mcp"},
      wantRest:       []string{"-model", "foo", "-mcp"},
      wantPositional: ".",
    },
    {
      name:           "flag=value form untouched",
      args:           []string{"-model=foo", "."},
      wantRest:       []string{"-model=foo"},
      wantPositional: ".",
    },
    {
      name:           "-- stops extraction, everything from it on passed through verbatim",
      args:           []string{"-mcp", "--", "."},
      wantRest:       []string{"-mcp", "--", "."},
      wantPositional: "",
    },
    {
      name:           "a second bare token is left in rest, not swallowed",
      args:           []string{"a", "b"},
      wantRest:       []string{"b"},
      wantPositional: "a",
    },
    {
      name:           "path-like value after a value-taking flag isn't mistaken for the positional",
      args:           []string{"-root", ".", "-mcp"},
      wantRest:       []string{"-root", ".", "-mcp"},
      wantPositional: "",
    },
  }

  for _, tc := range cases {
    t.Run(tc.name, func(t *testing.T) {
      rest, positional := extractPositionalRoot(tc.args)
      if positional != tc.wantPositional {
        t.Errorf("positional = %q, want %q", positional, tc.wantPositional)
      }
      if !reflect.DeepEqual(rest, tc.wantRest) {
        t.Errorf("rest = %v, want %v", rest, tc.wantRest)
      }
    })
  }
}

func TestExtractResumeWithID(t *testing.T) {
  dir := t.TempDir()

  rest, pos := extractPositionalRoot([]string{ "-resume", "3f2a91c0", "-model", "quick" })
  if pos != "" || strings.Join(rest, " ") != "-resume=3f2a91c0 -model quick" {
    t.Fatalf("id: rest=%v pos=%q", rest, pos)
  }

  rest, pos = extractPositionalRoot([]string{ "-resume", dir })
  if pos != dir || strings.Join(rest, " ") != "-resume" {
    t.Fatalf("directory after -resume must stay the root: rest=%v pos=%q", rest, pos)
  }

  rest, pos = extractPositionalRoot([]string{ ".", "-resume" })
  if pos != "." || strings.Join(rest, " ") != "-resume" {
    t.Fatalf("bare -resume: rest=%v pos=%q", rest, pos)
  }

  var f resumeFlag
  f.Set("true")
  if f != "menu" {
    t.Fatalf("bare -resume = %q, want menu", f)
  }
}
