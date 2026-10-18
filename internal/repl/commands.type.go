// A slash command's entry in the table.

package repl

import (
  "context"
)

type command struct {
  names   []string // first entry is canonical, rest are aliases
  usage   string
  summary string
  run     func(r *REPL, ctx context.Context, args []string)
}
