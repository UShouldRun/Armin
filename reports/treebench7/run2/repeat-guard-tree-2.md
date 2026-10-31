I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 30540 prompt (23616 cached), 430 completion, $0.00308, 3 rounds, 3 tools

## answer


One line, one file:

**`internal/agent/turn.go:36`**
```go
const maxBlocked = 2
```
Change the `2` to `3`. That's the repeat guard's limit; it's checked at `turn.go:128` and `turn.go:290` (`if t.blocked >= maxBlocked`), and both compare against the constant, so no other code changes.

Two things to know:

- **No test edit needed**: `TestTurnStopsAModelThatKeepsRetrying` (`internal/agent/loop_test.go:548`) computes its expected call count as `2 + maxBlocked`, so it adapts automatically.
- **Optional doc sync**: `docs/components/agent.md:30` says "after two refusals the turn winds down" — update "two" to "three" if you want the docs accurate.

Want me to make the change?

## calls
search_code map[pattern:refus]
search_code map[pattern:refuseCount|refusedRepeat|refusals]
search_code map[path:internal pattern:maxBlocked]

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

