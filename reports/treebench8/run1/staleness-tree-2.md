When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 44309 prompt (30528 cached), 798 completion, $0.00316, 4 rounds, 4 tools

## answer









Dependent nodes are flagged **suspect**, and it's done by `(*know.Graph).MarkSuspect` (internal/know/stale.go:128-165).

The full flow, driven by `refreshKnow` (internal/think/stale.go:34-117):

1. **Direct staleness** — `CheckFile` (internal/know/stale.go:72-123) re-hashes the cited line ranges in changed files. If the content matches it's fine; if the same lines just moved in the file, it re-anchors the reference via `Reanchor`; otherwise it marks the node's own evidence as stale with `SetFlags(e.Node, FlagStale, 0)` (stale.go:114).

2. **Propagation to dependents** — the stale node list is handed to `MarkSuspect(stale)` (internal/think/stale.go:93). It does a **breadth-first walk over the reverse `requires` and `derived-from` edges** — i.e., everything that requires or was derived from a stale node, transitively. Each live node it reaches that isn't already stale or suspect gets `FlagSuspect` set (internal/know/stale.go:163). Already-stale nodes are enqueued but not re-flagged.

3. **Then** `FlagCycles` runs for requires-loop bookkeeping (internal/know/stale.go:171+).

The effect on the model: a node marked `STALE` or `SUSPECT` is never presented as "current" in tree lines (internal/know/line.go:65-68), so armin re-reads the code before trusting it again.

## calls
search_code map[path:internal/know pattern:stale]
read_file map[path:internal/know/stale.go]
search_code map[path:internal/think pattern:MarkSuspect]
read_file map[end_line:110 path:internal/think/stale.go start_line:30]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793475801300389026"
author a2 armin session "bench-1793475801300389026"

n1 know topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 know/staleness topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 know/staleness/detection fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "The knowledge tree notices changed/moved cited code at session start via think.refreshKnow (internal/think/thinal → stale.go:34): it stats each file cited by file evidence and compares its stamp (size, mtime, ctime, inode, from a \"stamps\" file beside the tree) with the last check, so only changed/gone files get re-hashed; VCS-free. A missing/damaged stamps file just re-hashes everything once."
  evidence file "internal/think/stale.go:34-125" 5d4162d4
  evidence file "internal/think/think.go:273" 6a1830f1

n4 know/staleness/classification fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "know.CheckFile (internal/know/stale.go:72) classifies each cited range of a changed file into three outcomes: unchanged (the range still hashes the same), moved within the file (a same-length window elsewhere in the file rolls to the same range hash via Roll, so the evidence is Reanchor'ed to the new lines), or stale (nothing matches, so the citing node gets FlagStale). Range hashes are polynomial over per-line FNV-1a hashes mod 2^32 (internal/know/hash.go), enabling O(file) rolling-window search. Stale nodes then propagate FlagSuspect breadth-first to everything resting on them via reverse requires/derived-from edges (MarkSuspect, stale.go:128)."
  derived-from n3
  evidence file "internal/know/stale.go:72-157" 0bca12c6
  evidence file "internal/know/hash.go:1-92" 3ca872f4
  evidence file "internal/know/graph.go:463" bcbe4639

n5 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n6 sessions/how-does-the-knowledge file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:34 internal/think/think.go:273 internal/know/stale.go:26 stale.go:141-216 internal/know/stale.go:72 stale.go:95 internal/know/hash.go:61 stale.go:101 stale.go:114 hash.go:8-39 stale.go:128 internal/know/line.go:81"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

