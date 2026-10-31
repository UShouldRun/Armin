I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 46237 prompt (37888 cached), 745 completion, $0.00694, 5 rounds, 4 tools

## answer
One line: in **`internal/agent/turn.go`**, line 36:

```go
const maxBlocked = 2
```

change to:

```go
const maxBlocked = 3
```

That constant is the only place the limit is set — the loop checks `t.blocked >= maxBlocked` at `turn.go:128` (stopping the turn) and `turn.go:290` (skipping the rest of a reply's calls), so both pick up the new value automatically.

Nothing else needs changing:

- `internal/agent/loop_test.go:548` computes its expected call count from `maxBlocked` itself, so `TestTurnStopsAModelThatKeepsRetrying` stays correct.
- Optionally update the doc to match: `docs/components/agent.md:30` says "after two refusals the turn winds down" — that should say three.
- The `repeatNote` text and `docs/design/metrics.md` (which just counts `guard.blocked` events) don't mention the number.

## calls
search_code map[pattern:refus]
search_code map[file_type:go pattern:refus]
read_file map[end_line:60 path:internal/agent/turn.go start_line:25]
search_code map[file_type:go path:internal/agent pattern:maxBlocked]

## tree after

