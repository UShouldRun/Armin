I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 77651 prompt (67328 cached), 1255 completion, $0.01126, 7 rounds, 7 tools

## answer
Found it. The limit lives in one constant, plus two places that hardcode the current value:

**1. The limit itself — `internal/agent/turn.go:34`**
```go
const maxBlocked = 2
```
Change to `const maxBlocked = 3`. That's the single source: `t.blocked >= maxBlocked` is checked at turn.go:121 (loop break) and turn.go:281 (wind-down, where the remaining calls stop running).

**2. A test that hardcodes the resulting call count — `internal/agent/loop_test.go:646-649`**
`TestTurnStopsIdenticalReruns` currently expects the model to be called 4 times (`ls`, blocked 1, blocked 2 → stop, summary). With the limit at 3 that becomes 5 calls, so update the comment and the assertion:
```go
// ls, ls (same: blocked 1), ls (same: blocked 2), ls (same: blocked 3, stop), summary.
if calls != 5 {
  t.Fatalf("model called %d times, want 5", calls)
}
```
The other retry test, `TestTurnStopsAModelThatKeepsRetrying` (loop_test.go:548), references `maxBlocked` symbolically, so it adjusts itself.

**3. Docs (optional but keep consistent) — `docs/components/agent.md:30`**
"after two refusals the turn winds down" → "after three refusals the turn winds down."

Note: with `maxBlocked = 3` the turn stops after exactly 3 refused repeats — matches your wording. If you instead meant 3 extra *tolerated* repeats on top of the current 2, use 4. Want me to make the changes?

## calls
search_code map[pattern:refus]
search_code map[pattern:repeat]
search_code map[]
search_code map[file_type:go pattern:maxBlocked]
read_file map[end_line:675 path:internal/agent/loop_test.go start_line:630]
read_file map[end_line:630 path:internal/agent/loop_test.go start_line:605]
read_file map[end_line:652 path:internal/agent/loop_test.go start_line:615]

## tree after

