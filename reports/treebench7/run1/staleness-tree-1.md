How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right false, 83935 prompt (46528 cached), 1485 completion, $0.01091, 7 rounds, 10 tools

## answer


It's a two-layer system: a cheap stamp check in `internal/think`, and hash-based content checking in `internal/know`.

**1. Finding which files changed — `refreshKnow` (internal/think/stale.go:30)**
At session start it stats every file cited by file evidence (size, mtime, ctime, inode) and compares against a `stamps` file stored beside the tree (internal/think/stale.go:97-140). No VCS involved. Files with an unchanged stamp are skipped entirely; changed or deleted files get re-read. A missing/corrupt stamps file means everything gets re-hashed once.

**2. Deciding what happened to the cited lines — `Graph.CheckFile` (internal/know/stale.go:59)**
Each file evidence (`path:12-40`) carries a hash computed when the node was written: FNV-1a per line, combined into a polynomial rolling hash mod 2³² with an odd base so it can be rolled in O(1) per line (internal/know/hash.go:16-60). For each evidence, three outcomes:
- **Unchanged**: the range at the old position still hashes to the stored value → nothing happens.
- **Moved within the file**: `Roll` slides a same-length window down the file comparing rolling hashes, preferring the match nearest the old position (so repeated code lands back where it was) → `Reanchor` (internal/know/graph.go:463) points the evidence at the new lines, and it counts as "moved", not stale.
- **Actually changed or file gone**: no matching window → the citing node gets `FlagStale` (it then prints as `STALE` and never as `current`).

Note the hash is only ever compared with the same evidence's earlier value — it's a change detector, not an identity (internal/know/hash.go:8-9).

**3. Propagating the damage — `MarkSuspect` (internal/know/stale.go:122)**
Breadth-first over the *reverse* `requires` and `derived-from` edges (`g.In`), starting from the newly stale nodes: every live node that directly or transitively rests on them gets `FlagSuspect` unless already stale/suspect. So a fact derived from a concept whose code changed is flagged without re-checking its claim.

**4. Housekeeping — `FlagCycles` (internal/know/stale.go:158)**
Keeps one question node per open `requires` loop under `cycles/`, deleting them when the loop resolves.

So "moved" is handled gracefully by re-anchoring; only genuinely edited or deleted lines make nodes stale, and staleness propagates up the dependency edges. Cost is O(changed evidence + lines of changed files + affected subgraph).



(Recorded the mechanism as n2 in the knowledge tree for next time.)

## calls
list_directory map[depth:3 path:internal/know]
semantic_search map[query:detect stale evidence cited file lines changed or moved]
file_stat map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
read_file map[path:internal/know/stale.go]
find_references map[name:CheckFile]
find_references map[name:Reanchor]
read_file map[path:internal/think/stale.go]
read_file map[end_line:500 path:internal/know/graph.go start_line:455]
know_write map[claim:How the knowledge tree notices cited code changed or moved: refreshKnow (internal/think/stale.go:30) stats every cited file (size/…

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

