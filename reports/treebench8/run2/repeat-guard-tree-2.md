I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 43404 prompt (39136 cached), 781 completion, $0.00193, 5 rounds, 5 tools

## answer


One line. In `internal/agent/turn.go:39`:

```go
// maxBlocked is how many repeats the guard refuses before the turn
// stops: a model that keeps retrying a blocked call won't stop on
// its own.
const maxBlocked = 2   // → const maxBlocked = 3
```

That's the whole limit. The guard counts repeats in `turn.run` (`internal/agent/turn.go:285-340`) against it, and at `maxBlocked` refusals it winds the turn down with `repeatNote` and tools off.

The test that pins this behavior, `TestTurnStopsAModelThatKeepsRetrying` (`internal/agent/loop_test.go:508`), computes its expectation from the constant itself (`calls != 1 + 1 + maxBlocked` at line 548), so it adapts and still passes — no test edit needed.

Want me to make the change and run the tests?

## calls
search_code map[path:internal/agent pattern:refus]
read_file map[end_line:200 path:internal/agent/aids.go start_line:100]
read_file map[end_line:45 path:internal/agent/turn.go start_line:30]
search_code map[path:internal/agent/loop_test.go pattern:maxBlocked]
read_file map[end_line:560 path:internal/agent/loop_test.go start_line:504]

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

