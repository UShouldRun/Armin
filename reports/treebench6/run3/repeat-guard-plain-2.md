I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 46848 prompt (38912 cached), 653 completion, $0.00672, 5 rounds, 6 tools

## answer
The limit is a single constant in `internal/agent/turn.go:34`:

```go
const maxBlocked = 2   // → change to 3
```

Both enforcement points already use the constant — the loop break at `turn.go:121` (`t.blocked >= maxBlocked`) and the wind-down error at `turn.go:281` — so that's the only production code change.

Two follow-on places to keep consistent:

- `internal/agent/loop_test.go:648-650` — `TestTurnStopsIdenticalReruns` hardcodes the expectation (`ls, ls (blocked 1), ls (blocked 2, stop), summary → calls == 4`). With the limit at 3 it becomes 5 calls; update the comment and the `4`. The sibling test `TestTurnStopsAModelThatKeepsRetrying` (loop_test.go:548) computes `2 + maxBlocked` symbolically, so it adjusts on its own.
- `docs/components/agent.md:30` — "after two refusals the turn winds down" — update to "three" if you want the docs accurate.

## calls
search_code map[pattern:refus]
semantic_search map[query:turn tools off after repeated refusals limit]
search_code map[pattern:maxBlocked]
read_file map[end_line_area:45 end_line_val:45 path:internal/agent/turn.go start_line:25]
read_file map[end_line:45 path:internal/agent/turn.go start_line:25]
search_code map[path:internal/agent/loop_test.go pattern:calls == 4|blocked]

## tree after

