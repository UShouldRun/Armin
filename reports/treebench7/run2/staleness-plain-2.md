When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 67412 prompt (58368 cached), 751 completion, $0.00894, 7 rounds, 7 tools

## answer
They get flagged **suspect** — not deleted or re-verified. The dependent nodes stay in the tree with their evidence; a `FlagSuspect` bit is set on each, so the model (and the node-line format, `!suspect`) knows their basis changed and they should be re-checked.

The function: `(*Graph).MarkSuspect` in `internal/know/stale.go:128`.

How it works:

- It takes the just-marked-stale node IDs and does a **breadth-first search over the reverse edges** — every node that has a `requires` or `derived-from` edge pointing at the stale node is a dependent (via `g.In`, internal/know/stale.go:154).
- Each newly reached live node gets `FlagSuspect` set (internal/know/stale.go:167), unless it's already stale or suspect.
- It's visited as a bitset, so it stops at the first reaching of any node.
- It returns the count it marked; the caller is `refreshKnow` in `internal/think/stale.go:93`, which records it as the `stale.suspect` metric.

So the semantics: the stale node's *own* evidence is gone (flag stale), and everything resting on it directly or transitively — at any depth — becomes suspect but untouched, pending re-verification.

## calls
semantic_search map[query:knowledge tree node becomes stale, invalidate dependent nodes]
search_code map[pattern:stale]
search_code map[path:internal/think pattern:suspect]
find_definition map[name:MarkSuspect]
search_code map[path:internal/know pattern:MarkSuspect|suspect]
read_file map[end_line175:175 path:internal/know/stale.go start_line:120]
read_file map[end_line:170 path:internal/know/stale.go start_line:120]

## tree after

