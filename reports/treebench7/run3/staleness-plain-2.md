When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 43766 prompt (32512 cached), 692 completion, $0.00691, 4 rounds, 5 tools

## answer
When a node goes stale, its dependents don't go stale — they get flagged **suspect**. This is done by `Graph.MarkSuspect` in internal/know/stale.go:128.

What it does:

1. **Seed**: takes the stale node IDs (`from`), marks them seen, and enqueues them.
2. **BFS over reverse edges**: repeatedly takes a node from the queue and walks its `In` edges (internal/know/stale.go:160), keeping only edges of kind `EdgeRequires` or `EdgeDerivedFrom` — so "explain" or parent/outline edges don't propagate suspicion.
3. **Flag new victims**: for each incoming edge's target `e.To` that is live and not yet visited, it adds it to the queue (to keep the walk transitive), and if it isn't already `FlagStale` or `FlagSuspect`, it sets `FlagSuspect` via `g.SetFlags` (internal/know/stale.go:171).
4. **Returns** the count of nodes it newly marked.

So it's transitive (a node derived-from a node that requires the stale one is covered too), it skips already-stale/suspect nodes, and it only touches live nodes. The doc comment at the top of the file (internal/know/stale.go:7-8) sums it up: "breadth first over reverse requires and derived-from edges: everything resting on a stale node."

You can see the intended test semantics in internal/know/stale_test.go:55-59 — a node that merely *explains* the stale one is not flagged, while nodes one or more hops away via requires/derived-from are, and an already-stale dependent isn't re-counted.

## calls
search_code map[pattern:stale]
semantic_search map[query:knowledge tree node stale invalidate dependents]
search_code map[path:internal/know pattern:stale]
search_code map[path:internal/know pattern:depend]
read_file map[end_line:190 path:internal/know/stale.go start_line:124]

## tree after

