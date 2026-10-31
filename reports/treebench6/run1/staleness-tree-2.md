When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 40034 prompt (30208 cached), 554 completion, $0.00607, 4 rounds, 5 tools

## answer
They get marked **suspect**, not stale.

- **`CheckFile`** (internal/know/stale.go:76) flags only the node whose cited lines no longer hash-match and can't be re-anchored — that gets `FlagStale`.
- **`MarkSuspect`** (internal/know/stale.go:125) is the function that does it for dependents. It does a breadth-first walk over the *reverse* `requires` and `derived-from` edges, so it reaches every node that rests on the stale ones directly or transitively, and sets `FlagSuspect` on each (skipping ones already stale or suspect).
- The whole flow runs at session start from **`refreshKnow`** (internal/think/stale.go:25): it stats the cited files, hashes only the ones whose stamp changed, calls `CheckFile`, then passes the returned stale nodes to `MarkSuspect`.

So the distinction is: stale = "the code I cited is gone/changed", suspect = "something I depend on went stale, so my claim needs re-checking." Suspect nodes are what you see in `know_find` output and what `know_trace` walks back through when a conclusion looks wrong.

## calls
know_find map[query:stale node depends dependents]
search_code map[pattern:stale]
read_file map[path:internal/think/stale.go]
outline_file map[path:internal/know/stale.go]
read_file map[path:internal/know/stale.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473424976257091"
author a2 armin session "bench-1793473424976257091"
author a3 armin session "bench-1793473454840172327"

n1 internal topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 internal/know topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 internal/know/staleness-detection fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "The knowledge tree detects changed/moved cited code in two layers: refreshKnow (internal/think/stale.go) stats each cited file (size/mtime/ctime/inode) against last-check stamps stored in <tree-dir>/stamps, and only re-hashes changed files. know.CheckFile then classifies each file evidence: unchanged if the cited range's RangeHash still matches the stored hash, moved if Roll finds an equal hash window elsewhere in the file (preferring the nearest match) and re-anchors the ref, stale (FlagStale) otherwise. MarkSuspect then flags everything resting on stale nodes via reverse requires/derived-from edges. Line hashes are FNV-1a; range hash is a mod-2^32 polynomial (base 1000003) enabling O(1)-per-line window rolling. Hashes are only ever compared to the same evidence's prior value."
  evidence file "internal/think/stale.go:31-88" e5382c07
  evidence file "internal/know/stale.go:27-115" 51d64d66
  evidence file "internal/know/hash.go:11-80" 0e3d6d7c

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-the-knowledge file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/know/hash.go internal/know/stale.go internal/think/stale.go"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

n6 sessions/when-a-node-of file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/know/stale.go:76 internal/know/stale.go:125 internal/think/stale.go:25"
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

