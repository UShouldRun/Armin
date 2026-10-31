How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right false, 81187 prompt (53568 cached), 1519 completion, $0.00986, 7 rounds, 10 tools

## answer


Recorded the mechanism as [n3] in the knowledge tree.

## calls
know_find map[query:evidence staleness changed moved detection]
list_directory map[path:internal/know]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
search_code map[path:internal/know pattern:Hash\s*[:=]|\.Hash\b]
find_callers map[name:CheckFile]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know pattern:func .*AddEvidence|RefFile|RefSymbol]
find_callers map[name:refreshKnow]
know_write map[claim:The knowledge tree detects changed/moved cited code in two layers: refreshKnow (internal/think/stale.go) stats each cited file (si…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473424976257091"
author a2 armin session "bench-1793473424976257091"

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

