How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 53448 prompt (42640 cached), 1348 completion, $0.00302, 5 rounds, 8 tools

## answer


It's handled in two layers: a hard **repeat guard** in the loop itself, plus a softer **"stuck" reasoning aid** that nudge-reasons the model before things get desperate. All of it lives in `internal/agent/turn.go`.

**1. Exact-repeat guard (`turn.run`, turn.go:255-333)**
- Every call is keyed by tool name + canonicalized arguments (`callKey`, loop.go:204; JSON marshaling sorts map keys so equal args → equal key).
- Failed results are recorded in `t.failed[key]` (turn.go:316). If the model makes that exact call again **with nothing successful in between**, it is not executed — it gets a synthetic tool result: `{"error": "you already made this exact call this turn and it failed: ... Don't repeat it: change the arguments, use another tool, or stop..."}` (turn.go:270-276). A success in between clears `t.failed` (turn.go:323), since the working call may have fixed the dependency.
- Each refusal increments `t.blocked`. Once `t.blocked >= maxBlocked` (2, turn.go:34), the rest of the reply's calls don't run at all — they get `{"error": "not run: this turn is stopping"}` so every call still has a result (turn.go:278-283) — and the round loop breaks with `stopNote = repeatNote` (turn.go:121-124).

**2. Same-output loop detection for program runs (turn.go:318-321)**
- For `run_program`-type tools, successful outputs are kept in `t.ran[key]`. If the identical command gives byte-identical output **back to back** (`t.lastCall == key`), it's refused as a loop ("running it again won't change anything") and also counts toward `t.blocked`.
- If there were other calls in between (e.g. an edit + rebuild), it *does* run — legitimate — but with a note: "same output as the last time... if you changed something in between, it didn't change this result."

**3. Wind-down, not an error (turn.go:126-140)**
- When the guard trips (or the tool-round limit is hit), one final model call is made with **no tool definitions** and a system-style note (`repeatNote`: "tools are off for the rest of this turn... Tell the user what you were trying to do, what failed..."). So the user gets a plain-language report instead of an error or an infinite loop.

**4. The "stuck" aid (aids.go:137-181)** — a separate, earlier intervention, active when the aid is enabled:
- `stall.observe` counts consecutive "misses" (error results, failing tests/build, non-zero exit); a passing check resets the counter.
- After 3 misses in a row (`stallLimit`), it injects `stuckNote` as a user message: step back, state what's known, list 2–3 hypotheses, test the cheapest one. At most `maxNudges` (2) per turn (aids.go:143-144), checked via `t.stuck.observe(id, result)` in `turn.call` (turn.go:252), with the note added in the round loop (turn.go:114-118).

So the escalation ladder is: repeat refused once with an instruction to change approach → refused twice → turn is cut off with tools disabled and a forced summary; alongside that, 3 consecutive failures (of any kind) get a "step back and hypothesize" nudge, twice max per turn.

Tests for the guard are in `internal/agent/loop_test.go`, and for the stall logic in `internal/agent/aids_test.go:34-57`.

## calls
project_manifest map[]
semantic_search map[query:agent loop handling repeated failed tool calls]
search_code map[path:internal/agent pattern:repeat|duplicate|same (call|tool)|stuck|loop detect]
list_directory map[path:internal/agent]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent pattern:observe|stuck \{|type stuck]
search_code map[path:internal/agent pattern:func callKey|type stall|maxNudges =]
read_file map[end_line:190 path:internal/agent/aids.go start_line:120]

## tree after

