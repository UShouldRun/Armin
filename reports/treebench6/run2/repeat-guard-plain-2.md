I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 27159 prompt (19456 cached), 463 completion, $0.00445, 3 rounds, 3 tools

## answer
Change one constant:

- `internal/agent/turn.go:34` — `const maxBlocked = 2` → `const maxBlocked = 3`

That constant is the whole limit: `turn.go:121` breaks the loop with `repeatNote` when `t.blocked >= maxBlocked`, and `turn.go:281` is where later calls in a reply get the "not run: this turn is stopping" error. No other production code uses the number.

Two things to keep in mind:

- `internal/agent/loop_test.go:548` — `TestTurnStopsAModelThatKeepsRetrying` asserts `calls == 1 + 1 + maxBlocked` by referencing the constant, so it still compiles, but the scripted model fake likely only produces that many calls; you'll need one more retry in the fake so the test still exercises the cutoff at 3.
- `docs/components/agent.md:30` says "after two refusals the turn winds down" — update it to "three" if you want the docs to stay accurate.

## calls
search_code map[pattern:refus]
search_code map[pattern:tool(s)? (off|disable|disabl)]
search_code map[path:internal/agent pattern:maxBlocked]

## tree after

