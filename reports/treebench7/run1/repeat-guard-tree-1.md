How does armin's agent loop deal with a model that keeps repeating a tool call that already failed?

right true, 129787 prompt (108368 cached), 2154 completion, $0.00572, 8 rounds, 11 tools

## answer


There's a dedicated **repeat guard** around every tool execution, in `internal/agent/turn.go`. It works in three layers, all per-turn:

**1. Refuse the exact retry** (`turn.run`, turn.go:273-289)
Each call gets a key from tool name + JSON args (`callKey`, loop.go:204). If a call fails, its error is stored in `t.failed[key]`. When the model issues the *same* call again, it's never executed — it gets a canned error: *"you already made this exact call this turn and it failed… Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."* A successful call in between clears the map (`clear(t.failed)`, turn.go:344), since the success may have fixed what the failure depended on.

**2. Catch output loops on runs** (turn.go:315-335)
For program runs, the raw output is kept in `t.ran[key]`. Re-running the same run call back-to-back with identical output is refused as a loop ("running it again won't change anything"); re-running after other calls in between *does* run, but gets a note that the output is unchanged.

**3. Wind the turn down** (`maxBlocked = 2`, turn.go:33-36, 128-131)
Each refusal increments `t.blocked`. Once it hits 2, the loop breaks out of the tool rounds and makes **one last model call with no tools**, with `repeatNote`: *"tools are off for the rest of this turn… tell the user what you were trying to do, what failed, and how you'll continue."* So the user gets a report, not a hang or an error. Between refusals, the stuck-reasoning aid (`AidStuck`, aids.go:21) can also fire: the `stall` counter (aids.type.go:14) tracks consecutive misses, and once it fires, a `stuckNote` ("Step back: your last attempts failed without progress…") is injected as a user message.

Each refusal is also metered as `guard.blocked` (turn.go:278-279). Behavior is covered by `TestTurnBlocksARepeatedFailedCall`, `TestTurnStopsAModelThatKeepsRetrying`, and `TestTurnStopsIdenticalReruns` in loop_test.go.



This is the **repeat guard**, a per-turn mechanism wrapped around tool execution in `internal/agent/turn.go`. (I have a note on this from earlier — n3 agent/loop/repeat-guard; here's the current code behind it.)

**1. Exact-retry refusal** — `turn.run`, turn.go:273-289
Every call is keyed by name + JSON args (`callKey`, loop.go:204). If a call fails, its error is stored in `t.failed[key]`. When the model issues the *same* call again, it isn't executed — it's answered with a canned error: *"you already made this exact call this turn and it failed: … Don't repeat it: change the arguments, use another tool, or stop and tell the user what is blocking you."* Any successful call clears the map (turn.go:344), since a success may have fixed what the failure depended on.

**2. Output-loop detection for runs** — turn.go:315-336
Program runs keep their raw output in `t.ran[key]`. Re-running the same run call back-to-back with identical output is refused as a loop ("running it again won't change anything"); if other calls happened in between it *does* run, with a note that the output is unchanged from before.

**3. Wind-down after 2 refusals** — `maxBlocked = 2` (turn.go:33-36, 128-131)
Each refusal bumps `t.blocked`. At 2, the loop breaks out of tool rounds and makes **one last model call with no tools**, seeded with `repeatNote`: tools are off, tell the user what you were trying to do, what failed, and how to continue. The user gets a report, not a hang.

**Supporting nudge:** the `AidStuck` reasoning aid (aids.go:21) runs a `stall` counter (aids.type.go:14, `observe` at aids.go:153) over consecutive failed attempts; when it trips, a `stuckNote` — *"Step back: your last attempts failed without progress…"* — is injected as a user message (turn.go:121-126) before the hard wind-down.

Each refusal is also metered (`guard.blocked`, turn.go:278-279), and the behavior is pinned by `TestTurnBlocksARepeatedFailedCall`, `TestTurnStopsAModelThatKeepsRetrying`, and `TestTurnStopsIdenticalReruns` in loop_test.go.

Net effect: a small model that retries a failed call unchanged gets a corrective error the second time, a step-back nudge if it keeps missing, and is forced into a plain-text report by the second refusal — so repetition can't burn the whole round budget.

## calls
search_code map[pattern:repeat]
search_code map[pattern:agent.*loop|loop.*agent]
search_code map[path:internal/agent pattern:repeat|Repeat]
search_code map[path:internal/agent pattern:maxBlocked|blocked]
read_file map[end_line:340 path:internal/agent/turn.go start_line:255]
read_file map[end_line:150 path:internal/agent/turn.go start_line:95]
read_file map[end_line:50 path:internal/agent/turn.go start_line:14]
search_code map[path:internal/agent pattern:func.*stuck|stuck.observe|type stuck]
search_code map[path:internal/agent pattern:stuck]
search_code map[path:internal/agent pattern:type stall|func.*stall.*observe|func callKey]
know_write map[claim:The repeat guard refuses a tool call that is an exact retry (name+args key) of one that failed this turn, and refuses back-to-back…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474423248699773"
author a2 armin session "bench-1793474423248699773"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/loop topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/loop/repeat-guard concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "The repeat guard refuses a tool call that is an exact retry (name+args key) of one that failed this turn, and refuses back-to-back identical-output reruns of program runs. maxBlocked=2 refusals end the tool rounds; one final tool-less model call with repeatNote yields a report. A success clears t.failed."
  evidence file "internal/agent/turn.go:262-345" cbf57cbb
  evidence file "internal/agent/turn.go:33-44" ed85f7df
  evidence file "internal/agent/loop.go:204-206" 72860d65

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin's-agent file checked a2 born 2026-10-31 seen 2026-10-31
  claim "turn.go:273-289 loop.go:204 turn.go:344 turn.go:315-336 turn.go:33-36 aids.go:21 aids.type.go:14 aids.go:153 turn.go:121-126 turn.go:278-279"
  evidence file "internal/agent/turn.go:14-50" 65f386d6
  evidence file "internal/agent/turn.go:255-340" caccc6a2
  evidence file "internal/agent/turn.go:95-150" e425a23f

