When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 16795 prompt (10240 cached), 563 completion, $0.00364, 2 rounds, 2 tools

## answer
They get flagged **suspect** (they stay live but are marked for re-checking), and it's done by `Graph.MarkSuspect` in `internal/know/stale.go:165`.

The mechanics:

1. **`CheckFile`** (`internal/know/stale.go:83`) re-hashes each changed file's cited ranges. If a range is unchanged it's fine, if it moved within the file it gets re-anchored, and only if neither holds is the citing node set `FlagStale` — those are the nodes returned to the caller.
2. **`MarkSuspect(stale)`** (`internal/know/stale.go:165`) does a breadth-first walk **against** the `requires` and `derived-from` edges — i.e., starting from each stale node, it follows `g.In(id)` and looks at `e.To` (the nodes that *rest on* the stale node), directly or transitively. Every live node in that closure that isn't already stale or suspect gets `FlagSuspect` set; already-stale/suspect nodes are passed through but not double-counted. It returns how many it marked.
3. The whole pass is orchestrated by `refreshKnow` in `internal/think/stale.go:50` at session start: it stamps cited files, only re-hashes the ones whose stamp changed, feeds the newly stale nodes into `MarkSuspect`, then runs `FlagCycles` and reports something like "knowledge tree: 1 stale, 3 suspect".

So the short answer: dependents aren't invalidated — the whole subgraph that *requires* or was *derived from* a stale node is marked suspect via a BFS over those edges' reverse direction, by `MarkSuspect`.

## calls
read_file map[path:internal/know/stale.go]
read_file map[path:internal/think/stale.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474423248669055"
author a2 armin session "bench-1793474423248669055"

n1 know topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 know/staleness concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "How the knowledge tree notices cited code changed or moved: refreshKnow (internal/think/stale.go:30) stats every cited file (size/mtime/ctime/inode) against a \"stamps\" file beside the tree and re-reads only changed/gone files; Graph.CheckFile (internal/know/stale.go:59) compares each evidence's stored rolling hash (FNV-1a per line, polynomial over line hashes, base 1000003, mod 2³² — hash.go:16) — unchanged → no-op, same-length window found elsewhere in the file via Roll → Reanchor (graph.go:463) re-points the reference, no match → FlagStale on the node; MarkSuspect (stale.go:122) then BFS-walks reverse requires/derived-from edges marking dependents FlagSuspect; FlagCycles keeps one question node per open requires loop. Hash is only ever compared with the same evidence's earlier value, never used as identity."
  evidence file "internal/think/stale.go:1-140" 483baaff
  evidence file "internal/know/stale.go:25-180" 5c3f8abe
  evidence file "internal/know/hash.go:1-60" 2991e5de
  evidence file "internal/know/graph.go:461-474" 41c4098c

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-does-the-knowledge file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/know/graph.go:455-500 internal/know/hash.go internal/know/stale.go internal/think/stale.go"
  evidence file "internal/know/graph.go:455-500" 750355da
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

