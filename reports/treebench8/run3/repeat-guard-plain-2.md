I want the loop to tolerate 3 refused repeats before it turns tools off, instead of the current limit. What exactly do I change, in which file?

right true, 50223 prompt (38400 cached), 602 completion, $0.00733, 5 rounds, 6 tools

## answer
One constant to change:

**`internal/agent/turn.go:40`** — `const maxBlocked = 2` → `const maxBlocked = 3`

That's the only place the limit is set. Both places the loop consults it compare against this constant:
- `turn.go:132` — `if t.blocked >= maxBlocked` breaks the turn loop (winds down the turn)
- `turn.go:302` — same check for short-circuiting the rest of a reply's calls

Nothing else needs touching:
- `TestTurnStopsAModelThatKeepsRetrying` (`internal/agent/loop_test.go:548`) computes its expected call count as `1 + 1 + maxBlocked`, so it adapts automatically.
- The `repeatNote` message doesn't mention the number.

Optional: the doc at `docs/components/agent.md:30` says "after two refusals the turn winds down" — update to "three" for consistency.

## calls
semantic_search map[query:refused repeats limit turn off tools]
search_code map[pattern:refus]
search_code map[path:internal/agent pattern:refus]
read_file map[end_line:45 path:internal/agent/turn.go start_line:30]
find_references map[name:maxBlocked]
search_code map[file_type:go path:internal/agent pattern:maxBlocked|t.blocked|stopping]

## tree after

