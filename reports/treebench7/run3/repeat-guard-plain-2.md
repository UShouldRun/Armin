I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 33901 prompt (26368 cached), 834 completion, $0.00596, 4 rounds, 4 tools

## answer
The limit lives in one constant, plus one hardcoded test and one doc line:

**1. `internal/agent/turn.go:36`** — the actual limit:
```go
const maxBlocked = 2
```
Change to `3`. `turn.run()` increments `t.blocked` on each refused repeat (turn.go:290 and the loop break at turn.go:128) and both compare against `maxBlocked`, so that's the only code change needed.

**2. `internal/agent/loop_test.go:648-651`** — `TestTurnStopsIdenticalReruns` hardcodes the number of model calls instead of using `maxBlocked`:
```go
  // ls, ls (same: blocked 1), ls (same: blocked 2, stop), summary.
  if calls != 4 {
    t.Fatalf("model called %d times, want 4", calls)
  }
```
With the limit at 3 this becomes 5 calls (ls, blocked 1, blocked 2, blocked 3 → stop, summary) — update both the comment and the expected count.

The other test, `TestTurnStopsAModelThatKeepsRetrying` (loop_test.go:548), references `maxBlocked` symbolically, so it adjusts on its own.

**3. Optional, docs: `docs/components/agent.md:30`** — "after two refusals the turn winds down" describes the limit and would be stale; worth matching if you want docs consistent.

## calls
search_code map[pattern:refus]
semantic_search map[query:turn tools off after repeated refusals in agent loop]
search_code map[file_type:go pattern:maxBlocked]
read_file map[end_line:680 path:internal/agent/loop_test.go start_line:555]

## tree after

