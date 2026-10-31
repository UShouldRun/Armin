When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 27516 prompt (12864 cached), 708 completion, $0.00327, 3 rounds, 3 tools

## answer







They get flagged **suspect** — not deleted or re-checked. The function is `MarkSuspect` on the knowledge graph, at internal/know/stale.go:128:

- It does a breadth-first walk **backwards** over the `requires` and `derived-from` edges (i.e. over every node's incoming edges, `g.In(id)`), so it reaches every node that depends on the stale ones, directly or transitively (internal/know/stale.go:141-160).
- Each reached live node that isn't already `FlagStale` or `FlagSuspect` gets `FlagSuspect` set; it returns how many it marked.
- The stale nodes themselves stay `stale`; only their dependents are downgraded to `suspect`, meaning "this conclusion *might* be wrong, re-check it" rather than "this is known wrong."

The wiring: `refreshKnow` in internal/think/stale.go collects the newly-stale node IDs from `g.CheckFile` (the re-hash pass over changed cited files), then calls `g.MarkSuspect(stale)` at internal/think/stale.go:93, followed by `FlagCycles`. So the chain is: file changed → `CheckFile` marks the citing node stale → `MarkSuspect` propagates suspicion to everything resting on it.

## calls
read_file map[end_line:220 path:internal/know/stale.go start_line:1]
read_file map[end_line:120 path:internal/think/stale.go start_line:1]
search_code map[path:internal pattern:MarkSuspect]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519947413"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:38 internal/know/stale.go:28 stale.go:156 stale.go:171 stale.go:188 internal/know/stale.go:71 hash.go:61 stale.go:118"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

