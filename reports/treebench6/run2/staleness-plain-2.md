When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 49980 prompt (41472 cached), 682 completion, $0.00714, 5 rounds, 5 tools

## answer
When a node's evidence no longer matches the source, its dependents get flagged **suspect** — a lighter flag than stale, meaning "this fact was built on something that changed, re-check it."

The flow (driven by the `think` package):

1. `know.Graph.CheckFile` (internal/know/stale.go:80) re-hashes the cited line ranges in a changed file. If a range still hashes the same, nothing happens; if the lines moved within the file, the reference is re-anchored; otherwise the citing node gets the `FlagStale` set.
2. The freshly-stale node IDs are passed to **`know.Graph.MarkSuspect`** (internal/know/stale.go:128), which does a breadth-first search over the *reverse* `requires` and `derived-from` edges — i.e. everything that rests on the stale node, directly or transitively, at any depth. Each live node it reaches gets `FlagSuspect` set (nodes already stale or suspect are left alone, visited are tracked in a bitset).
3. Suspect nodes show up as `!suspect` in the tree lines, so the model knows to treat them as untrustworthy (e.g. `know_trace` sorts stale nodes first) instead of re-exploring blindly.

So the one function doing the "blame your dependents" work is `MarkSuspect`, called from `think` (internal/think/stale.go:83) right after `CheckFile` returns the newly-stale nodes.

## calls
semantic_search map[query:knowledge tree node stale invalidate dependents]
search_code map[pattern:stale]
search_code map[path:internal/know pattern:suspect]
read_file map[path:internal/know/stale.go]
find_callers map[name:MarkSuspect]

## tree after

