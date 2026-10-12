package ui

import (
  "strings"
  "testing"
)

func TestRenderMarkdown(t *testing.T) {
  Enabled = false
  defer func() { Enabled = true }()

  md := strings.Join([]string{
    "# Roadmap review",
    "Overall: **well gated**, see `schema.sql` and [the doc](https://x.dev).",
    "- first point",
    "  - nested *point*",
    "1. numbered",
    "- [x] done task",
    "> a quote",
    "```go",
    "x := **not bold**",
    "```",
    "| a | b |",
    "|---|---|",
    "| `c` | d |",
    "---",
    "snake_case_name and a*b stay as they are",
  }, "\n")

  got := RenderMarkdown(md, 100)
  for _, want := range []string{
    "Roadmap review\n",
    "Overall: well gated, see schema.sql and the doc (https://x.dev).",
    "• first point", "  • nested point", "1. numbered", "✓ done task",
    "│ a quote", "┌─ go", "│ x := **not bold**", "└─",
    "a │ b", "c │ d",
    "snake_case_name and a*b stay as they are",
  } {
    if !strings.Contains(got, want) {
      t.Fatalf("missing %q in:\n%s", want, got)
    }
  }

  if strings.Contains(got, "# ") || strings.Contains(got, "**well") {
    t.Fatalf("markers left in:\n%s", got)
  }
}

func TestWrapKeepsHangingIndent(t *testing.T) {
  Enabled = false
  defer func() { Enabled = true }()

  inCode := false
  rows := RenderLine("- one two three four five six seven", &inCode, 16)
  if len(rows) < 2 || !strings.HasPrefix(rows[0], "• ") || !strings.HasPrefix(rows[1], "  ") {
    t.Fatalf("rows = %q", rows)
  }

  for _, r := range rows {
    if len([]rune(r)) > 16 {
      t.Fatalf("row too wide: %q", r)
    }
  }
}
