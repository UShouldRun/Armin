// The report armin -metrics prints: every metric of the cumulative
// file grouped by system, then the derived rates that say whether a
// system helps (recall cited, dedup held, stamps skipping reads...).

package metrics

import (
  "fmt"
  "io"
  "path/filepath"
  "sort"
  "strings"
)

// rates are the derived figures, num per den.
var rates = []rate{
  { "recall attached, of turns", "recall.attached", "recall.turns" },
  {
    "recall cited by the model, of attached",
    "recall.cited",
    "recall.attached",
  },
  {
    "recall's files read without a citation, of attached",
    "recall.read",
    "recall.attached",
  },
  { "dedup held a write back, of adds", "know.dedup.held", "know.write.add" },
  {
    "dedup overridden (force), of held",
    "know.dedup.forced",
    "know.dedup.held",
  },
  { "auto-recorded turns, of turns", "know.record.turns", "turn.count" },
  {
    "pointers consolidated, of pending",
    "know.consolidate.pointers",
    "know.consolidate.pending",
  },
  {
    "consolidated claims linked to an existing node, of claims",
    "know.consolidate.linked",
    "know.consolidate.claims",
  },
  {
    "files skipped by stamp, of cited",
    "stale.files.skipped",
    "stale.files.cited",
  },
  { "moved code followed, of changed refs", "stale.moved", "stale.changed" },
  {
    "delegation checks passed, of run",
    "delegate.check.passed",
    "delegate.check.run",
  },
  { "repeat guard blocks, of tool calls", "guard.blocked", "tool.calls" },
  { "flat recordings refused, of recordings", "stt.flat", "stt.recordings" },
}

// Report prints the cumulative metrics in dir.
func Report(w io.Writer, dir string) error {
  all := read(filepath.Join(dir, "metrics.tsv"))
  if len(all) == 0 {
    _, err := fmt.Fprintf(w, "no metrics yet in %s\n", dir)
    return err
  }

  groups := map[string][]string{}
  for name := range all {
    sys, _, _ := strings.Cut(name, ".")
    groups[sys] = append(groups[sys], name)
  }

  var systems []string
  for sys := range groups {
    systems = append(systems, sys)
  }

  sort.Strings(systems)
  for _, sys := range systems {
    fmt.Fprintf(w, "%s\n", sys)
    names := groups[sys]
    sort.Strings(names)
    for _, name := range names {
      s := all[name]
      if s.Sum == 0 && s.Max <= 0 {
        fmt.Fprintf(w, "  %-36s %8d\n", name, s.Count)
        continue
      }

      fmt.Fprintf(
        w,
        "  %-36s %8d  avg %-9.4g min %-9.4g max %.4g\n",
        name, s.Count,
        s.Sum / float64(s.Count),
        s.Min, s.Max,
      )
    }
  }

  fmt.Fprintf(w, "\nrates\n")
  for _, r := range rates {
    num, den := all[r.num], all[r.den]
    if den == nil || den.Count == 0 {
      continue
    }

    n := int64(0)
    if num != nil {
      n = num.Count
    }

    fmt.Fprintf(
      w,
      "  %-42s %5.1f%%  (%d of %d)\n",
      r.label,
      100 * float64(n) / float64(den.Count),
      n, den.Count,
    )
  }

  return nil
}
