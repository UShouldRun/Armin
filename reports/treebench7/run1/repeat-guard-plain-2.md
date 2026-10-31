I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 32816 prompt (27392 cached), 617 completion, $0.00509, 4 rounds, 4 tools

## answer
The limit is a single constant, **`maxBlocked`**, in **`internal/agent/turn.go`**:

```go
// turn.go:33-36
// maxBlocked is how many repeats the guard refuses before the turn
// stops: a model that keeps retrying a blocked call won't stop on
// its own.
const maxBlocked = 2
```

Change `2` → `3`. Everything else keys off the constant:

- `internal/agent/turn.go:128` and `:290` — the two `t.blocked >= maxBlocked` checks (stop the round loop / stop running further calls) use the constant, so no other code change is needed.
- `internal/agent/loop_test.go:548` — the test computes its expectation as `2 + maxBlocked`, so it adapts automatically.

One doc line goes stale: `docs/components/agent.md:30` says "after two refusals the turn winds down" — update it to three if you care about doc accuracy.

## calls
search_code map[pattern:refus]
semantic_search map[query:stop tools when the model repeatedly refuses to comply]
search_code map[path:internal pattern:refusal|refusedRepeats|windDown|wind-down]
search_code map[path:internal pattern:maxBlocked]

## tree after

