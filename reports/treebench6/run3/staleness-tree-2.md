When a node of the knowledge tree goes stale, what happens to the nodes that depend on it, and which function does it?

right true, 40587 prompt (30976 cached), 746 completion, $0.00658, 4 rounds, 4 tools

## answer
Dependents are flagged **suspect**, and it's done by `(*know.Graph).MarkSuspect` (internal/know/stale.go:128), called from `refreshKnow` (internal/think/stale.go:83) as the second step of a refresh.

The flow:

1. **`refreshKnow`** (internal/think/stale.go:32) checks each file cited by node evidence. `CheckFile` either re-anchors a moved reference (same-length window rolled across the file) or returns the node as **stale** when its evidence no longer matches.
2. **`g.MarkSuspect(stale)`** (internal/know/stale.go:83) then does a BFS over the **reverse** `requires` and `derived-from` edges (`g.In(id)`), marking every live node that depends on a stale node — directly or transitively — with `FlagSuspect` via `SetFlags`, skipping nodes already stale or suspect and using a bitset (`seen`) to avoid revisits. It returns how many it marked.
3. The counts go into the refresh message: `N stale`, `N suspect`, `N references followed moved code`, `N open requires loops` (internal/think/stale.go:105-112).

The doc comment on `MarkSuspect` states it exactly: "flags as suspect every live node that requires or was derived from one of `from`, directly or not, and isn't stale or suspect already."

Note the asymmetry: a dependent that is itself stale stays stale (stale outranks suspect); suspect nodes are softer — `know_find` treats them as "re-check before trusting," and re-verification through `know_write` (`verified`/`current`) clears them.

## calls
search_code map[pattern:stale]
search_code map[file_type:go pattern:suspect]
read_file map[end_line:175 path:internal/know/stale.go start_line:120]
read_file map[end_line:120 path:internal/think/stale.go start_line:30]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245545058"
author a2 armin session "bench-1793473527245545058"
author a3 armin session "bench-1793473564125260903"

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

n5 sessions/when-a-node-of file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/know/stale.go:128 internal/think/stale.go:83 internal/think/stale.go:32 internal/know/stale.go:83 internal/think/stale.go:105-112"
  evidence file "internal/know/stale.go:120-175" c76258b1
  evidence file "internal/think/stale.go:30-120" 38ade054

