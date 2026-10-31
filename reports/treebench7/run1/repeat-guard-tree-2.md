I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 30619 prompt (7840 cached), 646 completion, $0.00249, 3 rounds, 3 tools

## answer


One line in `internal/agent/turn.go`. The limit is the constant:

```go
// turn.go:33-36
// maxBlocked is how many repeats the guard refuses before the turn
// stops: ...
const maxBlocked = 2
```

Change `maxBlocked = 2` to `maxBlocked = 3` at `internal/agent/turn.go:36`.

That's the only code change that matters:

- `t.blocked >= maxBlocked` at `turn.go:128` is where the loop stops issuing tool calls, and `t.blocked++` at `turn.go:277` and `turn.go:320` counts each refused repeat (exact repeat of a failed call, or the same program output right after the same run). With `3`, the model gets three refusals before tools switch off for the rest of the turn.
- Tests already key off the constant — `loop_test.go:548` computes `1 + 1 + maxBlocked` — so they follow the new value. One expectation to eyeball: `loop_test.go:648-649` hardcodes `calls != 4` for a scenario that hits the stop at 2 blocks; with the limit at 3 that test's model needs one more blocked call, so that literal will need to go to 5 (or the scripted model needs an extra call) for the test to pass.

Docs that would also want touching (not code): `docs/components/agent.md:30` says "after two refusals the turn winds down" — update to three to keep it accurate.

## calls
search_code map[pattern:refus]
search_code map[pattern:tools off|disableTools|toolsDisabled]
search_code map[path:internal/agent pattern:maxBlocked|blocked]

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

