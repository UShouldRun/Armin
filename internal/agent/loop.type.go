// The Loop's configuration and state, the Hooks a frontend passes to
// a turn, a Proposal shown before a write or a run, and a turn's state.

package agent

import (
  "sync"

  "armin/internal/ctags"
  "armin/internal/library"
  "armin/internal/llm"
  "armin/internal/notes"
  "armin/internal/sandbox"
  "armin/internal/searxng"
  "armin/internal/tools"
)

// Loop wires an LLM client to the sandboxed tool implementations.
type Loop struct {
  LLM     *llm.Client
  Sandbox *sandbox.Config

  // WebSearch backs the web_search tool. Nil disables it gracefully
  // — the tool is still advertised (see ToolDefinitions), but every
  // call returns a clean "unavailable" error instead of a panic.
  WebSearch *searxng.Manager

  // Symbols backs the ctags-based tools; nil degrades the same way
  // WebSearch does.
  Symbols *ctags.Index

  // Tracker remembers what the model has read, so write_file and
  // edit_file never act on a file it hasn't seen (or that changed
  // since).
  Tracker *tools.FileTracker

  // Memories, Skills and Scratch back the note tools; each one left
  // nil drops its tools from the definitions.
  Memories *notes.Memories
  Skills   *notes.Skills
  Scratch  *notes.Scratchpad

  // Library backs library_search and library_topics; nil drops
  // them from the definitions.
  Library *library.Library

  // Aids are the reasoning aids turns get (see AidAll); Procedures
  // the steps per kind of task (DefaultProcedures when nil).
  Aids       Aids
  Procedures map[Task]string

  // MaxRounds overrides DefaultMaxRounds.
  MaxRounds int

  // ContextSize is the model's context window in tokens; zero
  // disables automatic compaction. Ignored when Window is set.
  ContextSize int

  // Window, if set, is a model with a resident and a streaming
  // context tier; manageContext moves between them (see Decide).
  Window WindowControl

  // KeepTurns is how many recent user turns keep their full tool
  // results; older ones are reduced to a one-line stub (see
  // elideTurn). Zero uses DefaultKeepTurns, negative disables.
  KeepTurns int

  // ReadOnly drops write_file/edit_file (-lsp has nobody to approve
  // a write).
  ReadOnly bool

  defsOnce   sync.Once
  defs       []llm.ToolDefinition
  params     map[string]map[string]bool // tool → its parameter names
  defsTokens int // rough size of defs, sent with every request
}

// Hooks are the per-call callbacks of Turn. They're parameters, not
// Loop fields: lspserver runs concurrent turns against one Loop.
// Every field may be nil.
type Hooks struct {
  // Token and Reasoning receive streamed content and thinking.
  Token, Reasoning llm.OnToken

  // ToolCall fires just before each tool call executes.
  ToolCall func(name string, args map[string]any)

  // ToolResult fires after it, with the result as sent to the model
  // (JSON); ResultLine summarizes it for display.
  ToolResult func(name, result string)

  // Approve is asked before a file write, with a diff preview. A
  // false return declines it; feedback, if any, goes to the model.
  // Nil approves everything.
  Approve func(tool, preview string) (ok bool, feedback string)

  // Steer, if set, is asked before each model call after the first
  // and before ending the turn: messages the user sent meanwhile,
  // which go in as user messages the model reads at that step.
  Steer func() []string

  // Propose, if set, gets what's about to happen in full — a file
  // change or a command — just before Approve (or just before it
  // runs, when nothing asks), so a frontend can show it properly.
  Propose func(p Proposal)

  // Compacted fires after Turn rewrote history in place (compaction
  // or elision), so the caller can resync anything mirroring it.
  Compacted func(before, after int)

  // Usage receives the server's token count after each model call.
  Usage func(llm.Usage)

  // Notice receives one-line status messages about context
  // management (growing the window, saving notes, compacting).
  Notice func(string)

  // Progress receives the model's prompt-reading progress (tokens
  // read of total) before each reply starts, where supported.
  Progress func(done, total int)

  // Drafting hears a tool call's name as the model starts writing
  // it: the stretch between thinking and ToolCall.
  Drafting func(name string)

  // Effort, if set, is how hard the model thinks this turn (an
  // Effort's Wire value); empty keeps the model's default.
  Effort string

  // Task is the kind of request (see Assess), for its procedure.
  Task Task

  // Aside receives what the reasoning aids do (stepping back,
  // checking the work), one line each.
  Aside func(string)
}

// Proposal is a write or a run about to happen.
type Proposal struct {
  Tool string // edit_file, write_file, run_program, ...

  // A file change: Path (as the model named it), Resolved (on
  // disk), the whole Old and New content, and the change itself.
  Path, Resolved string
  Old, New       string
  Created        bool
  Start          int      // first changed line, 0-based
  Gone, Came     []string // removed and added lines
  OldLines       []string
  NewLines       []string

  // A run: the command line, where it runs, and the script it
  // runs, if one of its arguments is a file in the project.
  Command, Dir, Script string
}

// turn is one Turn's state across its rounds.
type turn struct {
  l        *Loop
  hooks    Hooks
  client   *llm.Client // l.LLM, or a copy at the turn's effort
  history  *[]llm.Message
  start    int // where this turn's messages begin in history
  sources  *turnSources

  // the repeat guard (see run): call key → its error since the last
  // success, run calls → their last result, the call before
  failed   map[string]string
  ran      map[string]string
  lastCall string
  blocked  int // repeats refused this turn

  declined map[string]bool // directories the user refused this turn
  stuck    stall
  verified bool
}
