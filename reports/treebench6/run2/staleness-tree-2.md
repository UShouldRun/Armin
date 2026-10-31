When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 33545 prompt (10496 cached), 451 completion, $0.00400, 3 rounds, 3 tools

## answer


When a node goes stale, its dependents get flagged **SUSPECT** (not stale — STALE means "its own cited lines changed"; SUSPECT means "something it rests on did"). The function that does it is `MarkSuspect` (`internal/know/stale.go:126`).

How it works:
- `CheckFile` (stale.go:70) first re-checks each changed file's cited ranges — unchanged or merely moved lines are re-anchored, and only genuinely changed lines call `SetFlags(node, FlagStale, 0)` and collect the newly-stale nodes to return.
- `MarkSuspect(from []NodeID)` then walks **breadth-first over reverse `requires` and `derived-from` edges** (`g.In(id)`), starting from the stale nodes. Every live node it reaches that isn't already STALE or SUSPECT gets `SetFlags(id, FlagSuspect, 0)`.
- The flags surface in `know_find` output as `STALE`/`SUSPECT` on the node's line (internal/know/line.go:81-82); a STALE/SUSPECT node never prints `current`, and the prompt tells the model both mean "re-check" rather than trust.

## calls
search_code map[pattern:stale]
search_code map[pattern:STALE]
read_file map[path:internal/know/stale.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473478108096017"
author a2 armin session "bench-1793473501741147416"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:27 internal/think/think.go:265 internal/know/stale.go:24 internal/think/stale.type.go:9 internal/know/stale.go:59 internal/know/line.go:67 internal/know/hash.go:55 internal/know/graph.go:463 internal/know/stale.go:112 internal/think/stale.go:88-101"
  evidence file "internal/know/graph.go:455-495" 4fc1337c
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000
  evidence file "internal/think/think.go:245-285" 55878440

n3 sessions/when-a-node-of file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/know/stale.go:126 stale.go:70 internal/know/line.go:81-82"
  evidence session "internal/know/stale.go" 00000000

