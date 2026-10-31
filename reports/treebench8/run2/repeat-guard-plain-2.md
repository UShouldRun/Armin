I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 36968 prompt (28160 cached), 586 completion, $0.00576, 4 rounds, 4 tools

## answer
It's one constant, in `internal/agent/turn.go`:

- **`internal/agent/turn.go:40`** — change `const maxBlocked = 2` to `3`.

That's the only behavioral change. The two checks that use it (`turn.go:132` — winds the turn down; `turn.go:302` — makes the rest of the reply's calls "not run") key off the constant, so they pick up the new limit automatically.

Two housekeeping notes:

1. **Tests**: `internal/agent/loop_test.go` asserts on the constant rather than a literal — `loop_test.go:548` uses `maxBlocked` directly, and `loop_test.go:649` hardcodes "want 4" for a scenario that fires `ls, ls, ls` (now the 3rd refusal doesn't stop the turn). If you bump the constant, `TestTurnStopsAModelThatKeepsRetrying` at `loop_test.go:648-650` will need one more `ls` scripted so it still reaches the stop, or its expected call count updated.
2. **Docs**: `docs/components/agent.md:30` says "after two refusals the turn winds down" — worth a one-word update to "three".

## calls
search_code map[pattern:refus]
semantic_search map[query:loop tolerates refused repeats then turns tools off]
search_code map[path:internal pattern:refus]
search_code map[path:internal/agent pattern:maxBlocked|blocked]

## tree after

