How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 62288 prompt (54336 cached), 1426 completion, $0.00419, 6 rounds, 8 tools

## answer















It works in two layers: cheap detection (file stamps) in `internal/think`, then hash-based re-verification in `internal/know`.

**1. What's recorded.** Each `RefFile` evidence (`path:12-40`) stores a hash of the exact lines it cites — per-line FNV-1a hashes combined into a polynomial range hash, `internal/know/hash.go:14-46`. The hash is only ever compared against that evidence's own earlier value.

**2. Which files to re-check.** At session start, `refreshKnow` (`internal/think/stale.go:37`) gets every cited file via `Graph.CitedFiles()` (`internal/know/stale.go:24`), stats each one (size, mtime, ctime, inode — `stampOf`, `stale.go:145`), and compares against the last check's stamps saved in a `stamps` file beside the tree. No version control involved; if the stamp is unchanged the file is skipped entirely, so cost stays O(cited files) stats.

**3. What happened to the cited code.** Only files whose stamp changed (or that are gone) are re-read and passed to `CheckFile` (`internal/know/stale.go:59`). For each piece of evidence it has three outcomes:
- **Unchanged** — the cited range still hashes the same: nothing to do (`stale.go:88`).
- **Moved within the file** — the original range's hash no longer matches, so it rolls a same-length window over the whole file with the polynomial hash, O(1) per line (`Roll`, `hash.go:57`), preferring the match nearest the old position (repeated code can match in several places). If found, the evidence is re-anchored to the new lines via `Reanchor` (`stale.go:101`), and the node stays current.
- **Genuinely changed (or file deleted)** — no matching window, so the node gets `FlagStale`, which is what shows up as STALE in the tree lines.

**4. Blast radius.** `MarkSuspect` (`stale.go:120`) then walks reverse `requires` and `derived-from` edges breadth-first and flags everything that rests on a stale node as SUSPECT (unless already stale/suspect). `FlagCycles` handles the requires-loop bookkeeping separately.

So: a rename/move of the file itself isn't specially detected — a gone file means all its evidence goes stale — but pure code motion *within* a file is transparently followed by the rolling hash. A touched-but-unchanged file (e.g. `git checkout` updating mtime) just fails the stamp comparison and gets re-hashed, then passes cleanly.

## calls
list_directory map[path:internal/know]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
search_code map[path:internal/know pattern:Reanchor]
read_file map[path:internal/think/stale.go]
read_file map[end_line:60 path:internal/know/line.go]
read_file map[end_line:40 path:internal/know/doc.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031593827526"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/know/hash.go:14-46 internal/think/stale.go:37 internal/know/stale.go:24 stale.go:145 internal/know/stale.go:59 stale.go:88 hash.go:57 stale.go:101 stale.go:120"
  evidence session "internal/know/doc.go" 00000000
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/line.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

