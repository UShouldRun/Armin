How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 139454 prompt (109568 cached), 1938 completion, $0.01384, 10 rounds, 14 tools

## answer


Now let me see who orchestrates this — the caller that decides which files changed.




I now have the full picture. Let me record this in the knowledge tree for next time.



It's a two-layer mechanism: cheap stamps to decide *which files to look at*, then content hashes to decide *what happened to each cited range*.

**1. Trigger: session start, not file watching**
`refreshKnow` (internal/think/stale.go:34) runs when a session opens the tree (internal/think/think.go:273). It's VCS-free by design — same code whether you use git, jj, or nothing.

**2. Which files changed: stamps**
For every file cited by live `RefFile` evidence (`g.CitedFiles()`, stale.go:22), it `stat`s the file and compares a `stamp` — size, mtime, ctime, inode (internal/think/stale.type.go:9) — against the last check's stamps, persisted in a `stamps` file beside the tree. Ctime+inode matter because `touch`/`cp -p`/`rsync -a` can preserve size+mtime, but not change-time or inode. Unchanged files are skipped entirely; only changed or gone ones are read and hashed.

**3. What happened to the code: rehash + roll (internal/know/stale.go:72)**
Each piece of evidence stores a hash of its cited lines: per-line FNV-1a combined into a polynomial range hash mod 2³² (internal/know/hash.go). For each cited range in a changed file, `CheckFile` classifies it:

- **Unchanged** — the cited lines still hash the same → nothing done.
- **Moved within the file** — `Roll` (hash.go:48) scans the file rolling the window hash in O(1) per line, preferring the match nearest the old start (repeated code may match several places). If found, `Reanchor` (graph.go:463) repoints the evidence to `path:newstart-newend`, logged as an op, and the move is counted.
- **Stale** — file gone, or no same-length window matches → the node gets `FlagStale`.

**4. Propagation through the tree**
`MarkSuspect` (stale.go:128) then does a breadth-first walk over *reverse* `requires` and `derived-from` edges, flagging everything that rests (directly or transitively) on a stale node as `FlagSuspect`. `FlagCycles` reconciles open requires loops with question nodes under `cycles/`.

So the final tree message ("2 stale, 1 suspect, 3 references followed moved code") and the `current`/`STALE`/`SUSPECT` markers you see on nodes (internal/know/line.go:72-81) come from that one session-start pass.

## calls
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:stale|Stale]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:CitedFiles]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/think pattern:type stamp struct|refreshKnow\(]
read_file map[path:internal/think/stale.type.go]
read_file map[end_line:285 path:internal/think/think.go start_line:255]
search_code map[path:internal/know pattern:func (g \*Graph) Reanchor]
search_code map[path:internal/know pattern:Reanchor]
know_write map[claim:The knowledge tree notices changed/moved cited code at session start via think.refreshKnow (internal/think/stale.go): it stats eve…
know_write map[claim:know.CheckFile (internal/know/stale.go:72) classifies each cited range of a changed file into three outcomes: unchanged (range has…

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

