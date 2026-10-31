How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 49602 prompt (36688 cached), 1037 completion, $0.00258, 5 rounds, 5 tools

## answer


It's handled by a **repeat guard** around tool execution in `internal/agent/turn.go`, plus a graceful wind-down of the turn.

## The guard: `t.run` (turn.go:257)

Each call is identified by `callKey` (loop.go:204 — tool name + JSON-marshalled args, sorted keys so equal args give equal keys). The guard keeps three maps: `failed` (key → last error), `ran` (key → last raw output), and a `blocked` counter. The rules:

1. **Identical failed call retried** — if the same call is made again and it's in `t.failed`, it is **refused without running**: the model gets `{"error":"you already made this exact call this turn and it failed: ... Don't repeat it: change the arguments, use another tool, or stop"}`. `t.blocked++`.
   - A success *in between* clears the map (`clear(t.failed)` after any non-error result), because the success may have fixed what the failed call depended on.
2. **Identical run-tool call with identical output** — back-to-back with nothing in between is refused as a loop (with the previous output attached); after an edit/rebuild it's legitimate, so it runs but gets a `note` that the output didn't change.
3. **Wind-down at `maxBlocked = 2`** (turn.go:30) — after two refusals, `t.blocked >= maxBlocked`, the rest of that reply's calls get `{"error":"not run: this turn is stopping"}` (every call still needs a result message), and the loop in `Turn` (turn.go:120) breaks with `stopNote = repeatNote`.

## The turn ending

When the guard trips the turn doesn't die with an error: it appends the `repeatNote` ("tools are off for the rest of this turn... tell the user what you were trying to do, what failed, and ask how to continue") and makes **one last model call with `nil` tool definitions** (turn.go:128), so the user gets a plain-text report. Same path as the 50-round limit (`roundLimitNote`).

## The "stuck" aid (aids.go)

A second, independent safety net: `stall.observe` (aids.go:153) counts consecutive *misses* (errors, failing tests/builds, non-zero exits). After `stallLimit = 3` misses in a row it injects `stuckNote` (aids.go:137) — "say what you know for sure, list 2–3 hypotheses, test the cheapest one" — capped at `maxNudges` per turn. This catches the broader "model keeps failing at things" case, not just exact repeats.

So: exact repeat → refused with an instruction; twice → tools off, forced report to the user; repeated general failure → step-back nudge up to a couple of times.

## calls
semantic_search map[query:detect repeated identical tool call that failed, stop retrying loop]
search_code map[pattern:repeat|stuck|same tool call]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent pattern:func callKey|func (s \*stuck)|func newStuck|func errorOf|stuckNote]
search_code map[path:internal/agent/aids.go pattern:observe|miss|AidStuck]

## tree after

