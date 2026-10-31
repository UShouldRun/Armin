When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 30105 prompt (16464 cached), 560 completion, $0.00533, 3 rounds, 3 tools

## answer


They get flagged **suspect**, not stale — and the function is `MarkSuspect` in `internal/know/stale.go:128` [n2].

How it works:

- When a cited file changes and a node's evidence no longer matches, `CheckFile` (`internal/know/stale.go:72`) marks that node `FlagStale` (direct, confirmed staleness).
- `refreshKnow` (`internal/think/stale.go:93`) then hands the list of newly stale nodes to `g.MarkSuspect(stale)`.
- `MarkSuspect` does a **breadth-first walk over reverse `requires` and `derived-from` edges** starting from the stale nodes. Every live node that requires a stale node, or was derived from one — directly or transitively — gets `FlagSuspect` set, if it isn't already stale or suspect. It returns how many it marked.

So the distinction is deliberate: the node whose evidence broke is *stale* (its lines no longer hash right, or were deleted), while everything *resting on* it is merely *suspect* — unverified, needs re-checking, but its own evidence may be untouched. The tree prints this as `!stale` vs `!suspect` per node, and the session-start summary reports the counts (e.g. "2 stale, 1 suspect" in `stale_test.go:80`).

## calls
search_code map[path:internal/know pattern:stale]
search_code map[path:internal/think pattern:[Ss]tale]
read_file map[end_line:180 path:internal/know/stale.go start_line:124]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031593827526"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/know/hash.go:14-46 internal/think/stale.go:37 internal/know/stale.go:24 stale.go:145 internal/know/stale.go:59 stale.go:88 hash.go:57 stale.go:101 stale.go:120"
  evidence session "internal/know/doc.go" 00000000
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/line.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

