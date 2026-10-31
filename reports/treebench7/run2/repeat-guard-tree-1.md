How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 83523 prompt (69584 cached), 1268 completion, $0.00352, 6 rounds, 7 tools

## answer


It has a dedicated "repeat guard" in `internal/agent/turn.go` (`turn.run`), layered with a "stuck aid". Here's the mechanism:

**1. Refusing the identical failed call** (`turn.go:273-290`)
Every tool call gets a key from name + marshaled args (`callKey`, loop.go:204). When a call fails, its error is stored in `t.failed[key]`. If the model issues that exact same call again, it's **not executed** — instead it gets a synthetic tool result: `"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user."` Each refusal increments `t.blocked` (tracked as the `guard.blocked` metric).

Key detail: any *successful* call clears the whole `t.failed` map (`turn.go:332`). So a retry after a fix is legitimate and runs; only a retry "with nothing working in between" is blocked.

**2. Loop detection for identical successful runs** (`turn.go:310-330`)
For run tools (build, test, program runs): if a run gives byte-identical output to its previous run, back-to-back with no other call in between, it's refused as a loop with the same-result error. If other calls went in between (e.g. you edited a file then re-ran), it runs but gets a `note` that "same output … if you changed something in between, it didn't change this result" — catching edits that silently didn't take effect.

**3. Wind-down after two refusals** (`turn.go:128-131`, `maxBlocked = 2`)
A model that keeps re-sending a blocked call won't stop on its own, so once `t.blocked >= 2` the loop breaks, and the rest of the reply's calls still get a `"not run: this turn is stopping"` result (every tool call needs one). The turn then makes **one final call with no tools at all**, sending `repeatNote`: "You kept repeating a call that had already failed, so tools are off for the rest of this turn… Tell the user what you were trying to do, what failed, and … how you'll follow it, then ask how to continue." So the user gets a report, not a silent stall or an error.

**4. The stuck aid nudges earlier** (`aids.go:137-190`)
Separately from the guard, the `stall.observe` helper counts misses in a row (errors, failing tests/builds, non-zero exits) and resets on a passing check. Three misses in a row inject a `stuckNote` user message: step back, state what you know from the tool output, list 2–3 hypotheses, test the cheapest one. At most two nudges per turn (`maxNudges`), so this can't loop either.

So the escalation ladder is: nudge ("step back, hypothesize") → refuse the identical call with an error message → refuse again → tools off, forced closing summary. There's a test for the first step: `TestTurnBlocksARepeatedFailedCall` in `loop_test.go:339`.

## calls
search_code map[pattern:repeat]
project_manifest map[]
search_code map[path:internal/agent pattern:guard|repeated]
list_directory map[path:internal/agent]
read_file map[path:internal/agent/turn.go]
search_code map[path:internal/agent pattern:stuckNote|func.*stuck|callKey|runTool]
read_file map[end_line:190 path:internal/agent/aids.go start_line:90]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474471069334962"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "turn.go:273-290 loop.go:204 turn.go:332 turn.go:310-330 turn.go:128-131 aids.go:137-190 loop_test.go:339"
  evidence file "internal/agent/aids.go:90-190" f3b2b65e
  evidence session "internal/agent/turn.go" 00000000

