How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 76806 prompt (68640 cached), 1421 completion, $0.00348, 7 rounds, 9 tools

## answer


It's handled by a "repeat guard" that lives on the `turn` struct (one turn = one user request across all model rounds), with three escalating layers:

**1. Refuse the exact same failed call** — `turn.run` (internal/agent/turn.go:273). Each call is keyed by tool name + canonicalized JSON args (`callKey`, internal/agent/loop.go:204). When a call's result contains an error, the key → error is stored in `t.failed`. If the same key is issued again, it's not executed at all; the model gets back a synthetic error: "you already made this exact call this turn and it failed… Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you." A call that *succeeds* clears the map, so retrying after a fix in between is allowed (turn.go:289, 344).

**2. Refuse identical-output program loops** — for `run`-family calls, the last successful output is remembered in `t.ran`. Same output back-to-back (nothing in between) is refused as a loop; same output with other calls in between (e.g., re-running after an edit) is allowed but annotated "same output as the last time you ran this… if you changed something, it didn't take effect" (turn.go:309-343).

**3. Wind down the turn** — every refusal increments `t.blocked`; at `maxBlocked = 2` (turn.go:36) the remaining tool calls in that reply get a `"not run: this turn is stopping"` stub (every call needs a result), and the loop breaks with `stopNote = repeatNote` — a user-role message telling the model tools are off for the rest of the turn, answer without tools (turn.go:128-143). The final tool-less model call gives the user a report instead of a silent hang or round-limit error.

There's also an orthogonal "stuck" aid (internal/agent/aids.go:137): when `t.stuck.observe` sees the same tool failing repeatedly, the loop injects a "Step back: say what you know for sure… list hypotheses" note (turn.go:116-124) — coaching before the hard wind-down kicks in.

All of this is per-turn state (`blocked`, `failed`, `ran` live on the `turn` struct, internal/agent/loop.type.go:199-205), so nothing carries across turns. It's counted as `guard.blocked` in metrics, and covered by `TestTurnStopsAModelThatKeepsRetrying` (internal/agent/loop_test.go:508), which asserts the model gets exactly 1 + 1 + maxBlocked calls and ends in a tool-less answer.

## calls
search_code map[pattern:repeated|repeat.*tool|duplicate.*call]
find_callers map[name:runTools]
search_code map[file_type:go pattern:repeatGuard|repeat guard|guard\.blocked]
read_file map[end_line:330 path:internal/agent/turn.go start_line:255]
read_file map[end_line:215 path:internal/agent/loop.type.go start_line:190]
read_file map[end_line:365 path:internal/agent/turn.go start_line:330]
search_code map[file_type:go path:internal/agent pattern:maxBlocked|func callKey]
read_file map[end_line:140 path:internal/agent/turn.go start_line:100]
search_code map[file_type:go path:internal/agent pattern:func (s \*?stall) observe|stuckNote =|repeatNote =]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474539443697272"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/turn.go:273 internal/agent/loop.go:204 turn.go:289 turn.go:309-343 turn.go:36 turn.go:128-143 internal/agent/aids.go:137 turn.go:116-124 internal/agent/loop.type.go:199-205 internal/agent/loop_test.go:508"
  evidence file "internal/agent/turn.go:100-140" f6bb66c1
  evidence file "internal/agent/turn.go:255-330" c79f90f5
  evidence file "internal/agent/turn.go:330-365" b71a0597

