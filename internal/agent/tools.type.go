// The tool table's entry, its run function and arguments.

package agent

import (
  "context"

  "armin/internal/llm"
)

// toolFlags say how armin treats a tool; one bit each.
type toolFlags uint16

// toolCall is one call as a run function sees it.
type toolCall struct {
  name  string
  raw   []byte // the arguments, as JSON
  hooks Hooks
}

// runFunc runs a call and returns a JSON-marshalable result; an
// error goes in the result as {"error": ...}, for the model to read.
type runFunc func(ctx context.Context, l *Loop, c toolCall) any

// toolSpec is everything armin knows about one tool, except how it's
// shown (display.go): what the model is told, how it runs, what it
// reads.
//
// def is the name, description and parameters; paths, if set, says
// which paths a call reads, for the access check; roles, if set, are
// the only roles offered the tool.
type toolSpec struct {
  id    ToolID
  flags toolFlags
  roles Role
  def   llm.ToolFunction
  run   runFunc
  paths func(args map[string]any) []string
}

// libraryArgs are library_search's and library_topics' arguments.
type libraryArgs struct {
  Query string `json:"query"`
  Count int    `json:"count"`
  Topic string `json:"topic"`
}

// noteArgs are the note tools' arguments; each uses some of them.
type noteArgs struct {
  Name        string `json:"name"`
  Description string `json:"description"`
  Content     string `json:"content"`
  Scope       string `json:"scope"`
  Append      bool   `json:"append"`
  StartLine   int    `json:"start_line"`
  EndLine     int    `json:"end_line"`
  File        string `json:"file"`
}

// knowArgs are the know tools' arguments; each uses some of them.
type knowArgs struct {
  Query       string   `json:"query"`
  Under       string   `json:"under"`
  Count       int      `json:"count"`
  IDs         []string `json:"ids"`
  ID          string   `json:"id"`
  Path        string   `json:"path"`
  Kind        string   `json:"kind"`
  Claim       *string  `json:"claim"`
  Confidence  string   `json:"confidence"`
  Evidence    []string `json:"evidence"`
  Needs       []string `json:"needs"`
  DerivedFrom []string `json:"derived_from"`
  Force       bool     `json:"force"`
  From        string   `json:"from"`
  To          string   `json:"to"`
  Remove      bool     `json:"remove"`
  MoveTo      string   `json:"move_to"`
  Verified    bool     `json:"verified"`
  Current     bool     `json:"current"`
  Delete      bool     `json:"delete"`
  Depth       int      `json:"depth"`
}
