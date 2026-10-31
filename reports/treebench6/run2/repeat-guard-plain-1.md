How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 93670 prompt (81824 cached), 1256 completion, $0.00364, 8 rounds, 8 tools

## answer


It's handled by a **per-turn repeat guard** in `turn.run` (`internal/agent/turn.go:266`), plus a step-back aid. The flow:

**1. Keying.** Every call is keyed by tool name + canonical JSON args (`callKey`, `internal/agent/loop.go:204`).

**2. Refusing failed repeats.** When a tool result contains an `"error"` field (`errorOf`, `loop.go:212`), the key goes into `t.failed` (turn.go:316). If the model sends the *exact same call* again, it's not executed — it gets a synthetic result back: *"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop…"* (turn.go:268–277). The refusal bumps `t.blocked`. A success in between clears `t.failed` (turn.go:324), so retrying a previously failed call *after* something worked (an edit, a rebuild) is allowed — that may have fixed what it depended on.

**3. Wind-down at `maxBlocked = 2`** (turn.go:34). After two refusals, any further calls — even new ones — are answered with `{"error": "not run: this turn is stopping"}` (each call must get a result), and the loop `break`s (turn.go:121–124).

**4. Closing call without tools.** Instead of an error, the turn ends with one final model call, `defs = nil` and no tools, prefaced with `repeatNote`: *"tools are off for the rest of this turn. Tell the user what you were trying to do, what failed, and … ask how to continue."* (turn.go:36, 127–132). The user gets a report, not a hang or a crash.

**Related but distinct:** for `run_*` tools, a successful call giving byte-identical output to the immediately previous identical call is also refused as a loop (turn.go:318–327) — but re-running the same program after other calls (e.g. an edit) is allowed, with a "same output as last time" note in case the change didn't take effect. Separately, the **stuck aid** (`aids.go:137`, `stall.observe`) counts generic failures — errors, failing tests/builds, non-zero exits — and injects a `stuckNote` telling the model to step back, state what it knows, and list hypotheses, before trying again (turn.go:114–119).

So: first repeat of a failure is a nudge, second is a stop, and the model is always forced to explain rather than loop.

## calls
project_manifest map[]
list_directory map[path:internal/agent]
search_code map[file_type:go path:internal/agent pattern:repeat]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent/aids.go pattern:stuck|observe|stuckNote|consecut]
search_code map[path:internal/agent/turn.go pattern:callKey]
search_code map[pattern:func callKey]
search_code map[file_type:go pattern:callKey|func errorOf|func runTool]

## tree after

