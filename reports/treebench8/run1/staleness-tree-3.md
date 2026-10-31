Where does armin keep the stamps of the files the tree cites, and what is in a stamp?

right true, 29530 prompt (19392 cached), 740 completion, $0.00264, 3 rounds, 3 tools

## answer







In the file **`stamps`**, sitting beside the knowledge tree itself — `filepath.Join(g.Dir(), "stamps")` (internal/think/stale.go:56), written back after every `refreshKnow` check via `writeStamps` (stale.go:110, 191-215, saved sorted by path through a `.tmp` + rename).

The on-disk format is: a uvarint file count, then per file a uvarint-prefixed path followed by the stamp as four little-endian 64-bit values (stale.go:8-10, 197-207). A stamp itself (stale.type.go:9-12, filled from `unix.Stat` in `stampOf`, stale.go:137-151) holds:

- **Size** — file size in bytes
- **Mtime** — modification time (nanoseconds)
- **Ctime** — change time (nanoseconds)
- **Ino** — inode number

Including ctime/inode means a replaced file is caught even if its size and mtime are preserved (touch -d, cp -p, rsync -a), per the comment in stale.type.go:7-8.

At the next session start, `refreshKnow` compares each cited file's current stamp against the stored one (stale.go:66-74); only files whose stamp changed or vanished get re-hashed by `know.CheckFile` — a missing or corrupt `stamps` file just degrades to re-hashing everything once (stale.go:153-155).

## calls
read_file map[path:internal/think/stale.go]
search_code map[path:internal pattern:stamp]
search_code map[path:internal/think pattern:type stamp struct]

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

