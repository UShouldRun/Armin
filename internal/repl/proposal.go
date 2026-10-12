package repl

import (
  "fmt"
  "path/filepath"
  "strings"

  "armin/internal/agent"
  "armin/internal/ui"
)

const (
  // proposalLines caps a change shown inline; [v]iew shows it all.
  proposalLines = 40
  scriptLines   = 15
  diffContext   = 2
)

// showProposal prints a write or a run before it happens, the way a
// reviewer reads it: "● Update(path)", what it adds and removes, the
// lines with their numbers.
func (r *REPL) showProposal(p agent.Proposal) {
  width := ui.TextWidth()
  if width <= 0 {
    width = 100
  }

  line := func(s string) { fmt.Println(ui.Fit(s, width + 4)) }

  switch {
  case p.Command != "":
    fmt.Println(ui.Bold("● Run") + "(" + p.Command + ")")
    where := "in " + r.rel(p.Dir)
    if p.Script == "" {
      fmt.Println(ui.Dim("  └ " + where))
      return
    }

    lines := strings.Split(strings.TrimRight(readSmall(p.Script), "\n"), "\n")
    fmt.Println(ui.Dim(fmt.Sprintf("  └ %s · runs %s (%d lines)", where, r.rel(p.Script), len(lines))))
    for i, l := range lines {
      if i == scriptLines {
        line(ui.Dim(fmt.Sprintf("      … %d more lines — v to read it all", len(lines) - i)))
        break
      }

      line(ui.Dim(fmt.Sprintf("    %4d  ", i + 1)) + l)
    }

  case p.Created:
    fmt.Println(ui.Bold("● Create") + "(" + p.Path + ")")
    fmt.Println(ui.Dim(fmt.Sprintf("  └ %d lines", len(p.NewLines))))
    for i, l := range p.NewLines {
      if i == proposalLines {
        line(ui.Dim(fmt.Sprintf("      … %d more lines — v to view all", len(p.NewLines) - i)))
        break
      }

      line(ui.Dim(fmt.Sprintf("    %4d ", i + 1)) + ui.Green("+ " + l))
    }

  default:
    fmt.Println(ui.Bold("● Update") + "(" + p.Path + ")")
    fmt.Println(ui.Dim("  └ " + plural(len(p.Came), "addition") + ", " + plural(len(p.Gone), "removal")))

    shown := 0
    emit := func(n int, mark, text string) bool {
      if shown == proposalLines {
        return false
      }

      num := ui.Dim(fmt.Sprintf("    %4d ", n))
      switch mark {
      case "-":
        line(num + ui.Red("- " + text))
      case "+":
        line(num + ui.Green("+ " + text))
      default:
        line(num + ui.Dim("  " + text))
      }

      shown++

      return true
    }

    from := max(0, p.Start - diffContext)
    for i := from; i < p.Start; i++ {
      emit(i + 1, " ", p.OldLines[i])
    }

    for i, l := range p.Gone {
      if !emit(p.Start + i + 1, "-", l) {
        break
      }
    }

    for i, l := range p.Came {
      if !emit(p.Start + i + 1, "+", l) {
        break
      }
    }

    after := p.Start + len(p.Gone)
    for i := after; i < min(len(p.OldLines), after + diffContext); i++ {
      emit(i + 1 - len(p.Gone) + len(p.Came), " ", p.OldLines[i])
    }

    total := len(p.Gone) + len(p.Came)
    if shown == proposalLines && total > shown {
      line(ui.Dim(fmt.Sprintf("      … %d changed lines in all — v to view the whole diff", total)))
    }
  }
}

// rel is path relative to the project, for display.
func (r *REPL) rel(path string) string {
  rel, err := filepath.Rel(r.Root, path)
  if err != nil || strings.HasPrefix(rel, "..") {
    return path
  }

  return rel
}

func plural(n int, word string) string {
  if n == 1 {
    return "1 " + word
  }

  return fmt.Sprintf("%d %ss", n, word)
}
