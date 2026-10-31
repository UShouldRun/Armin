How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 76160 prompt (57472 cached), 1429 completion, $0.00780, 6 rounds, 7 tools

## answer














There are two layers to it, both in `internal/agent`:

## 1. The repeat guard (`turn.run`, internal/agent/turn.go:285-340)

The comment says why it exists: *"Small models retry what failed, unchanged."* It works per turn, keyed by tool name + exact arguments (`callKey`, internal/agent/loop.go:204 — JSON marshaling sorts map keys, so equal args give equal keys):

- **Exact failed retry refused.** A failed call is remembered in `t.failed[key]` (turn.go:331). If the model makes the *identical* call again, it isn't executed — it gets a synthetic tool result instead: *"you already made this exact call this turn and it failed: \<prev error\>. Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."* (turn.go:288-298)
- **Success in between resets it.** A successful result clears `t.failed` (turn.go:334), so a retry after a fix is legitimate and runs.
- **Same-output run loop.** For runs (build/test/program), if a call succeeds but produces *byte-identical* output to the immediately preceding identical run, it's refused as a loop with the output included; if there were other calls in between it runs, but with a "nothing changed" note (turn.go:337-349).
- **Winding down.** Each refusal increments `t.blocked`. Once `t.blocked >= maxBlocked` (2, turn.go:40), every remaining call in the reply still gets a result — *"not run: this turn is stopping"* — because every tool call needs one, but nothing executes (turn.go:302-308).

## 2. Forcing the turn to end (internal/agent/turn.go:132-135, 140-160)

A model that keeps retrying a blocked call won't stop on its own, so when `t.blocked >= maxBlocked` the loop breaks out and makes **one final model call with tools off**, preceded by the `repeatNote` (turn.go:42-45): *"You kept repeating a call that had already failed, so tools are off for the rest of this turn. Tell the user what you were trying to do, what failed, ... and ask how to continue."* The user gets a report instead of an error or an infinite loop.

## 3. Related: the "stuck" aid (internal/agent/aids.go:145-180)

A slightly different signal — not exact repeats, but *progress* — the `stall` observer counts consecutive misses (error results, failing tests/builds, non-zero exits); a pass resets it. After `stallLimit = 3` misses it injects a `stuckNote` asking the model to step back: state what's certain, list 2-3 hypotheses, test the cheapest one. Capped at `maxNudges = 2` per turn so it can't itself become a loop. This is only active if `AidStuck` is enabled (it's the minion/delegate default, tools_delegate.go:144).

Blocks are counted as the `guard.blocked` metric (`guard.blocked` vs. `tool.calls` in the metrics report, internal/metrics/report.go:33-34), and the behavior is covered by `TestTurnStopsAModelThatKeepsRetrying` (internal/agent/loop_test.go:508) — the model ends with a tool-less answer after `1 + 1 + maxBlocked` calls, not at the round limit.

Documented for users in docs/guide/tools.md:206-210.

## calls
search_code map[pattern:consecutiveFail|repeated|repeti]
project_manifest map[]
search_code map[path:internal pattern:guard\.blocked|RepeatGuard|repeatGuard]
read_file map[end_line:400 path:internal/agent/turn.go start_line:180]
search_code map[path:internal/agent pattern:maxBlocked|func callKey|stuckObserve|AidStuck|observe]
read_file map[end_line:178 path:internal/agent/turn.go start_line:42]
read_file map[end_line:200 path:internal/agent/aids.go start_line:100]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519734905"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/turn.go:285-340 internal/agent/loop.go:204 turn.go:331 turn.go:288-298 turn.go:334 turn.go:337-349 turn.go:40 turn.go:302-308 internal/agent/turn.go:132-135 turn.go:42-45 internal/agent/aids.go:145-180 tools_delegate.go:144 internal/metrics/report.go:33-34 internal/agent/loop_test.go:508 docs/guide/tools.md:206-210"
  evidence file "internal/agent/aids.go:100-200" f3367d7a
  evidence file "internal/agent/turn.go:42-178" 104415ed

