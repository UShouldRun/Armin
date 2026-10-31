How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 145329 prompt (132352 cached), 1479 completion, $0.00490, 10 rounds, 12 tools

## answer


The guard lives in `internal/agent/turn.go`, around `t.run` (turn.go:257-336), with a per-turn counter in the turn struct (`loop.type.go:197`). The flow:

1. **Keying** — every call is keyed by tool name + canonicalized arguments (`callKey`, loop.go:204).

2. **Failed-repeat refusal** — successful errors are recorded in `t.failed[key]`. If the model makes the *exact same call again* and it failed before, it isn't executed; it gets a synthetic tool result: *"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user"* (turn.go:268-278). Each refusal bumps `t.blocked`. Any success in between clears `t.failed`, so a re-run after a fix is allowed.

3. **Hard stop at `maxBlocked = 2`** (turn.go:31-34) — once two repeats have been refused, the turn's remaining calls get `{"error":"not run: this turn is stopping"}` so every call still has a result, the loop breaks, and a final model call goes out **without tool definitions** with `repeatNote` (turn.go:39-43, 121-124, 137-150): the model must stop calling tools and report what it tried, what failed, and ask how to continue.

4. **A second, subtler case** — for program runs (successful but identical output), back-to-back identical output is refused as a loop; identical output with other calls in between (e.g. after an edit) is allowed but annotated "same output as last time: if you changed something, it didn't change this result" (turn.go:299-324).

5. **Softer nudge first** — independent of the hard guard, the `stuck` reasoning aid (`aids.go`) watches for consecutive failures per tool and injects `stuckNote` ("step back… list hypotheses… test the cheapest") rather than blocking anything.

The test covering this is in `loop_test.go:648` — the sequence `ls, ls (blocked 1), ls (blocked 2, stop), summary`. Note this limit (2) is exactly what the treebench "repeat-guard" follow-up question about raising it to 3 targets.

## calls
know_find map[query:agent loop repeated tool call failure retry]
search_code map[pattern:repeat.*tool|same tool|identical.*call]
search_code map[path:internal pattern:repeat|blocked|sameToolCall|dedup]
find_files map[path:internal pattern:turn.go]
search_code map[path:internal/agent pattern:blocked|same:|identical]
read_file map[end_line:140 path:internal/agent/turn.go start_line:25]
read_file map[end_line:345 path:internal/agent/turn.go start_line:240]
read_file map[end_line:240 path:internal/agent/turn.go start_line:160]
search_code map[path:internal/agent pattern:func callKey|func .*stuck|stuckNote =]
search_code map[path:internal/agent pattern:func (s \*stuck)|func (st stuck|type stuck]
search_code map[path:internal/agent pattern:stuck]
read_file map[end_line:145 path:internal/agent/aids.go start_line:85]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473527246460695"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "turn.go:257-336 loop.type.go:197 loop.go:204 turn.go:268-278 turn.go:31-34 turn.go:39-43 turn.go:299-324 loop_test.go:648"
  evidence file "internal/agent/aids.go:85-145" 78054790
  evidence file "internal/agent/turn.go:160-240" ac9c4d9a
  evidence file "internal/agent/turn.go:25-140" fac8ab7a

