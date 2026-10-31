When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 83902 prompt (71936 cached), 873 completion, $0.01105, 7 rounds, 9 tools

## answer
Dependents are flagged **suspect**, and the function that does it is `(*Graph).MarkSuspect` in `internal/know/stale.go:129`.

How it works:

- `CheckFile` (`internal/know/stale.go:75`) re-hashes the cited line ranges of changed files. If the lines just moved within the file, the evidence is re-anchored; otherwise the node gets `FlagStale`.
- `MarkSuspect(stale)` then does a breadth-first search over the **reverse** `requires` and `derived-from` edges from the newly-stale nodes — i.e. every live node that depends on a stale node, directly or transitively. Each one that isn't already stale/suspect gets `FlagSuspect` set, and it returns how many it marked.
- It's invoked from `refreshKnow` in `internal/think/stale.go:93`, right after collecting the stale nodes from `CheckFile`, before the cycle pass (`FlagCycles`).

So a stale node's dependents don't get deleted or re-verified — they're marked suspect so the tree (and the model reading node lines) can see their evidence changed and needs re-checking.

## calls
semantic_search map[count:10 query:knowledge tree node goes stale dependents invalidate]
search_code map[pattern:stale]
search_code map[path:internal/know pattern:invalidate|Invalidate]
search_code map[path:internal/know pattern:depend]
file_stat map[path:internal/know/stale.go]
outline_file map[path:internal/know/stale.go]
read_file map[path:internal/know/stale.go]
find_callers map[name:MarkSuspect]
read_file map[end_line:115 path:internal/think/stale.go start_line:60]

## tree after

