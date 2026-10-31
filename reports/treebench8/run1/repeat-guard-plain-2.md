I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 22826 prompt (17408 cached), 554 completion, $0.00404, 3 rounds, 3 tools

## answer
It's a single constant:

**File:** `internal/agent/turn.go:40`

```go
// before
const maxBlocked = 2
// after
const maxBlocked = 3
```

That's the whole change. `t.blocked` gets incremented on each refused repeat (turn.go:289 and turn.go:332) and both checks — the wind-down at turn.go:302 and the loop break at turn.go:132 — compare against `maxBlocked`, so no other code needs touching.

Two things to keep in mind:

- **Semantics:** the comparisons are `t.blocked >= maxBlocked`, so 3 means "tools turn off on the 3rd refused repeat" — i.e. it tolerates 2 refusals and stops on the 3rd. If you want it to only shut off *after* the 3rd (tolerate 3, stop on the 4th), use `4`. The docs phrase it as "after two refusals the turn winds down," which matches `maxBlocked = 2` meaning stop-on-the-2nd.
- **Test that will now fail:** `internal/agent/loop_test.go:648` hard-codes the stop-at-2 sequence (`ls, ls, ls, summary` → 4 calls). You'll need to add one more `ls` to that scripted sequence.

Also update `docs/components/agent.md:30`, which documents "after two refusals."

Want me to make the change and run the tests?

## calls
semantic_search map[query:refused repeats limit turn off tools]
search_code map[pattern:refus]
search_code map[path:internal/agent pattern:blocked]

## tree after

