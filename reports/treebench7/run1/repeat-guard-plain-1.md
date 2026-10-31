How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 55698 prompt (40032 cached), 1113 completion, $0.00285, 5 rounds, 7 tools

## answer


It's the **repeat guard** in `internal/agent/turn.go` — a per-turn mechanism built specifically because small models retry what failed, unchanged. It works in three layers:

**1. Same-call detection (`t.run`, turn.go:275)**
Every call is keyed by tool name + canonicalized arguments (`callKey`, loop.go:204). When a call fails (result contains an `"error"` field, via `errorOf`), the key and error are stored in `t.failed`. If the model makes that exact call again, it's **not run** — it gets a refusal JSON back instead:

> "you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."

Note the escape hatch: any successful call in between clears `t.failed` (turn.go:343), since a success may have fixed what the failed call depended on. So it only blocks a retry with *nothing working in between*.

**2. No-progress run detection (turn.go:320)**
For `run_program`-style tools (`isRun`), there's a second case: the call succeeded but produced **byte-identical output** to the previous run of the same key.
- Back-to-back (last call was the same key) → counted as a loop, refused with "running it again won't change anything."
- With other calls in between → it runs, but gets a note: "same output as the last time you ran this… if you changed something in between, it didn't change this result."

**3. Wind-down (`Turn`, turn.go:128)**
Each refusal increments `t.blocked`. At `maxBlocked = 2` (turn.go:36), the turn **stops** (`break`), and any remaining tool calls in that same reply get a dummy result — "not run: this turn is stopping" — so every call still gets a response. The turn then makes one final call **without tools**, with the `repeatNote` injected as a user message:

> "[armin] You kept repeating a call that had already failed, so tools are off for the rest of this turn… Tell the user what you were trying to do, what failed… then ask how to continue."

So the user gets a report/summary instead of an infinite loop or a raw error.

There's also a softer, complementary layer: the **stuck aid** (aids.go:137) — the `stall` observer counts "misses" (errors, failing tests/builds) and, if enabled, injects a `stuckNote` telling the model to step back and reason about what it knows before retrying. The repeat guard is the hard stop; the stuck aid is the soft nudge.

The behavior is tested in `internal/agent/loop_test.go` (`TestTurnBlocksARepeatedFailedCall`), and it's exercised in the treebench suite as the "repeat-guard" scenario.

## calls
search_code map[pattern:repe]
semantic_search map[query:detect repeated or looping tool calls that keep failing]
search_code map[path:internal/agent pattern:repeat|Refus|refus]
outline_file map[path:internal/agent/loop.go]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent/aids.go pattern:stuckNote|func.*stuck|observe]
search_code map[path:internal/agent pattern:func callKey|func errorOf]

## tree after

