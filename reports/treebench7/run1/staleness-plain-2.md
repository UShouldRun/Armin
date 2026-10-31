When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 46837 prompt (38912 cached), 682 completion, $0.00679, 5 rounds, 5 tools

## answer
They get marked **suspect**, and the function is `(*Graph).MarkSuspect` in `internal/know/stale.go:128`.

How it works:

- It starts from the list of stale node IDs and does a **BFS over the reverse edges** — for each node it pulls in the `requires` / `derived-from` edges (`g.In`) and walks the nodes that depend on it, transitively, not just direct dependents.
- Only **live** nodes that aren't already stale or suspect get the `FlagSuspect` set (via `SetFlags`); a bitset tracks what's been visited. It returns how many it marked.
- Callers: the start-of-session refresh in `internal/think/stale.go:93` — `refreshKnow` computes the stale list from the hash check, calls `g.MarkSuspect(stale)`, and reports it as the `stale.suspect` metric.

So a stale node's dependents are not discarded, just flagged suspect — the tree still shows them, but they're marked `!suspect` in the node line so the model knows their evidence may have changed and re-verifies them when it needs to.

## calls
search_code map[pattern:stale]
semantic_search map[query:knowledge tree node invalidated recompute dependent nodes]
search_code map[path:internal pattern:suspect]
read_file map[end_lineity:180 path:internal/know/stale.go start_line:120]
read_file map[end_line:175 path:internal/know/stale.go start_line:120]

## tree after

