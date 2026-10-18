# The agent

The tool-calling round trip at armin's center. Package
`internal/agent`. Every frontend (terminal, web, nvim) runs its turns
through one `Loop`.

## A turn

`Loop.Turn(history, hooks)` runs one user request (`turn.go`):

1. **Plan**: the frontend has already settled the effort and kind of
   task (`PlanTurn`, `effort.go`). The turn puts the effort line in
   the system prompt and the task's procedure on the request.
2. **Each round**:
   - what the user typed meanwhile goes in (`Hooks.Steer`)
   - the context is kept inside the window (`context.go`, see below)
   - the model is called with the tool definitions
   - if it **answered**, the work is checked once (the verify aid),
     the answer gets its web sources, and the turn ends
   - if it **called tools**, each call goes through the repeat guard
     and runs, and its result goes back into the history
3. **Out of rounds** (50 by default) or stopped for repeating itself:
   one last call without tools, so the user gets a report.

The **repeat guard** (`turn.run`) exists because small models retry
what failed, unchanged:
- a failed call made again with nothing working in between is refused;
- a program giving the same output right after the same run is refused
  as a loop;
- after two refusals the turn winds down.

`Hooks` are how a frontend watches and steers a turn: streamed tokens
and thinking, each tool call and result, approvals for writes and
runs, messages typed meanwhile.

## The tool table

Every tool the model can call is one `toolSpec` (`tools.go`). It
holds:
- what the model is told (name, description, parameters)
- flags (writes files, runs a program, needs the agent)
- how it runs
- which paths it reads

The specs are grouped by topic: files, search, code, git, run, notes.

- Most run through an adapter around a `tools.*` function; the rest
  need the agent: writes wait for approval, `run_program` too, notes
  use the Loop's stores.
- `toolid.go` fixes the order the model sees them in. A golden test
  (`testdata/definitions.json`) pins that text byte for byte, because
  a change alters model behavior and invalidates prompt caches.
- The MCP server serves the same table (`Loop.RunTool`).
- `display.go` renders calls and results for the screen.

## Context management

After every model call (`context.go`, `policy.go`):

| window used | what happens |
|---|---|
| < 50% | nothing: the prompt prefix stays byte-stable, so the server's cache keeps hitting |
| 50–75% | old tool results become one-line stubs, archived to the scratchpad first |
| 60–75% | the model offers the user a compaction at a good moment |
| > 75% | compact older history into a summary, or move to the larger streamed window when most of the history is the current task |

## Reasoning aids

`aids.go`, on by default (`aids =`):
- **stuck**: after three misses in a row, the model is asked to list
  hypotheses and test the cheapest one.
- **procedure**: steps for the kind of task, each opening with "check
  it can be done as asked".
- **verify**: one quick check of a turn that changed or ran something.

## Connections

- built by: `think.buildLoop`
- uses: `llm` (the model), `tools` and `sandbox` (the
  implementations), `notes`, `library`, `searxng`, `ctags`, `convo`
  (summaries)
- used by: `repl`, `web`, `lspserver`, `mcpserver`
