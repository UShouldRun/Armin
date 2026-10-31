How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right false, 144475 prompt (132736 cached), 2072 completion, $0.01254, 11 rounds, 14 tools

## answer


Recorded as [n2] in the tree for next time.

## calls
know_find map[query:stale evidence detection]
list_directory map[depth:3 path:internal/know]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:CitedFiles]
search_code map[path:internal/know pattern:func.*Reanchor|func.*SetFlags|func.*MarkSuspect]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know pattern:RefFile.*Add|AddEvidence|evidence.*hash|Hash:]
search_code map[path:internal pattern:AddEvidence\(]
read_file map[end_line:605 path:internal/agent/tools_know.go start_line:560]
find_callers map[name:refreshKnow]
read_file map[end_line:285 path:internal/think/think.go start_line:250]
know_write map[claim:The knowledge tree notices changed/moved cited code by storing, per file-range evidence, a polynomial hash over per-line FNV-1a ha…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245545058"
author a2 armin session "bench-1793473527245545058"

n1 know topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 know/staleness concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "The knowledge tree notices changed/moved cited code by storing, per file-range evidence, a polynomial hash over per-line FNV-1a hashes (odd base 1000003, so window hashes roll in O(1)). At session start refreshKnow compares stat stamps (size/mtime/ctime/inode, saved in a \"stamps\" file) of cited files; only changed files are re-hashed. A range that still hashes the same is fine; if its lines match elsewhere in the same file (Roll), the evidence is re-anchored (moved); otherwise the node is flagged stale and MarkSuspect flags everything that requires/derives from it (reverse BFS). Cross-file moves and deleted files simply look \"gone\" and go stale."
  evidence file "internal/know/hash.go:13-75" 5bcee787
  evidence file "internal/know/stale.go:28-172" 17740941
  evidence file "internal/think/stale.go:25-93" deaaa367
  evidence file "internal/think/think.go:265" 6a1830f1
  evidence file "internal/agent/tools_know.go:576-603" 3d81fdf9

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-does-the-knowledge file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools_know.go:560-605 internal/know/hash.go internal/know/stale.go internal/think/stale.go internal/think/think.go:250-285"
  evidence file "internal/agent/tools_know.go:560-605" a7d35ff8
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000
  evidence file "internal/think/think.go:250-285" f4be986b

