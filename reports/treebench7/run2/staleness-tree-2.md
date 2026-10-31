When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 45551 prompt (39488 cached), 967 completion, $0.00482, 5 rounds, 4 tools

## answer


When a knowledge-tree node goes stale, its dependents are flagged **SUSPECT** (not deleted, not re-verified, and not themselves "stale"). The distinction matters: `FlagStale` means "the node's *own* cited lines changed," while `FlagSuspect` means "something it *rests on* changed." The dependents stay in the tree with all their evidence; a flag bit is set so the model treats them as untrustworthy and re-checks them (node lines render as `!suspect`).

The function that does it is **`(*Graph).MarkSuspect`** — `internal/know/stale.go:128`.

How it works (`internal/know/stale.go:128-166`):
- It's a **BFS** seeded with the newly-stale node IDs.
- It walks the graph backwards following **incoming `requires` and `derived-from` edges only** — so a dependent of a dependent gets caught at any depth. Other edge kinds (`implements`, `explains`, `contradicts`) do not propagate.
- Each reached node must be `live` and not already `FlagStale`/`FlagSuspect`; otherwise it's skipped. Otherwise `SetFlags(..., FlagSuspect, 0)` is applied and counted.
- Returns the number of nodes it marked.

Where it's invoked: `refreshKnow` (`internal/think/stale.go:82`) calls `g.MarkSuspect(stale)` right after `g.CheckFile` returns the stale nodes — as part of the session-start staleness sweep. Then `FlagCycles` runs to detect `requires` loops among what was touched.

The whole sweep is driven from `refreshKnow` (`internal/think/stale.go:25-107`), which compares each cited file's stamp (size, inode, mtime, ctime) against the last saved `stamps` file and only re-hashes files whose stamp changed or that are gone.

## calls
read_file map[path:internal/think/stale.go]
find_definition map[from_file:internal/think/stale.go name:MarkSuspect]
search_code map[pattern:func.*MarkSuspect]
read_file map[end_line:175 path:internal/know/stale.go start_line:85]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474471070587789"
author a2 armin session "bench-1793474471070587789"

n1 know topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 know/staleness topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 know/staleness/detection concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "The knowledge tree notices changed/moved cited code at session start via think.refreshKnow (internal/think/stale.go): it stats every file cited by live RefFile evidence, compares a stamp (size, mtime, ctime, inode — ctime/inode catch replacement even when size+mtime are preserved) against stamps persisted in a \"stamps\" file beside the tree, and re-hashes only changed/gone files with know.CheckFile. No VCS involved; works with or without git."
  evidence file "internal/think/stale.go:34-106" 1c016783
  evidence file "internal/think/stale.type.go:9-12" bd1ab96e
  evidence file "internal/think/think.go:273" 6a1830f1

n4 know/staleness/reanchor-or-stale concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "know.CheckFile (internal/know/stale.go:72) classifies each cited range of a changed file into three outcomes: unchanged (range hash still matches, nothing done), moved within the file (the same-length window re-found via Roll, an O(file) rolling-hash scan preferring the match nearest the old start; the evidence is then Reanchor'd to the new path:lines and counted as moved), or stale (file gone, or no matching window: the node gets FlagStale). Hashing is per-line FNV-1a combined into a polynomial range hash mod 2^32 (internal/know/hash.go) so windows can be rolled in O(1) per line; it's only ever compared against the same evidence's earlier value. After CheckFile, MarkSuspect (stale.go:128) breadth-firsts over reverse requires/derived-from edges flagging everything resting on a stale node as FlagSuspect, and FlagCycles reconciles open requires loops with question nodes under \"cycles\"."
  derived-from n3
  evidence file "internal/know/stale.go:72-123" a265fdb8
  evidence file "internal/know/stale.go:128-167" abdd50d3
  evidence file "internal/know/hash.go:37-77" 1015e9d2

n5 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n6 sessions/how-does-the-knowledge file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:34 internal/think/think.go:273 stale.go:22 internal/think/stale.type.go:9 internal/know/stale.go:72 hash.go:48 graph.go:463 stale.go:128 internal/know/line.go:72-81"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000
  evidence session "internal/think/stale.type.go" 00000000
  evidence file "internal/think/think.go:255-285" 5b69428d

