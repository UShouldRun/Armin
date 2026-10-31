I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 33114 prompt (25600 cached), 545 completion, $0.00517, 4 rounds, 4 tools

## answer
One line: in `internal/agent/turn.go:40`, change

```go
const maxBlocked = 2
```

to

```go
const maxBlocked = 3
```

That's all:
- Both guard points — `turn.go:121` (break the loop with `stopNote = repeatNote`, i.e. tools off) and `turn.go:281` (refuse the remaining calls in the reply) — compare against `maxBlocked`, so they pick up the new value automatically.
- The test `TestTurnStopsAModelThatKeepsRetrying` (`internal/agent/loop_test.go:508`) computes its expected call count from `maxBlocked`, so it adapts on its own.
- The doc comment (`turn.go:37-40`) and `repeatNote` don't state the number, so nothing else changes.

Want me to make the edit and run the tests?

## calls
search_code map[path:internal/agent pattern:refus]
read_file map[end_line:50 path:internal/agent/turn.go start_line:30]
find_references map[name:maxBlocked]
read_file map[end_line:530 path:internal/agent/loop_test.go start_line:504]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300653491"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin's-agent file checked a1 born 2026-10-31 seen 2026-10-31
  claim "loop.go:204 turn.go:274 turn.go:292 turn.go:37-40 turn.go:138-149 aids.go:137-176 loop.type.go:200-205"
  evidence file "internal/agent/aids.go:90" e9b25bd7
  evidence file "internal/agent/turn.go:30" 6b1278ab

