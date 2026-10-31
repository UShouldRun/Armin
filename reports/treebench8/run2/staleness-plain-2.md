When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 66071 prompt (55040 cached), 883 completion, $0.00937, 6 rounds, 7 tools

## answer
Dependents get flagged **suspect**, not stale — done by `Graph.MarkSuspect` in `internal/know/stale.go:129`.

How it works:

1. **Direct stale marking** happens in `CheckFile` (`stale.go:72`): when a cited file's content re-hash doesn't match and can't be re-anchored (code just moved), the node gets `FlagStale` directly.
2. **Propagating to dependents** is `MarkSuspect(from []NodeID)`: a BFS starting from the newly stale nodes, walking the graph *inward* (`g.In`) and following only `EdgeRequires` and `EdgeDerivedFrom` edges. Every live node resting on a stale node, directly or transitively, gets `FlagSuspect` — but only if it isn't already stale or suspect (so a stale node never gets re-graded, and already-suspect ones aren't re-counted). It returns how many it newly marked.

The caller is `refreshKnow` in `internal/think/stale.go:43`, which runs the tree's staleness check at session start: stat-compare cited files against stored stamps, `CheckFile` the changed ones, then `MarkSuspect(stale)` and `FlagCycles`, and reports a summary like "knowledge tree: 2 stale, 5 suspect".

The two flags are deliberately distinct: stale = "the evidence I based this on no longer hashes to what I saw"; suspect = "something I rest on is stale, I may still be right but someone should re-check" (see `stale.go:4-8` doc). A node never says "current" on a short line if it's stale or suspect (`line.go:12-13`).

## calls
search_code map[pattern:stale]
semantic_search map[query:knowledge tree node stale invalidation dependents]
search_code map[path:internal/know pattern:stale|Stale]
list_directory map[path:internal/know]
read_file map[end_line:185 path:internal/know/stale.go start_line:125]
find_callers map[name:MarkSuspect]
read_file map[path:internal/think/stale.go]

## tree after

