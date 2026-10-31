How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 59336 prompt (53056 cached), 1140 completion, $0.00354, 6 rounds, 7 tools

## answer














It's a two-layer design: **think** detects *which* files changed; **know** re-verifies what each citation's lines actually say.

**1. Change detection — file stamps, no VCS needed** (`internal/think/stale.go:38` `refreshKnow`)
- Runs at session start. `g.CitedFiles()` (`internal/know/stale.go:28`) maps every live file-evidence citation to its cleaned path.
- For each cited path it stats the file and compares a stamp — size, mtime, ctime, inode (`stampOf`, `stale.go:156`) — against the last check's stamps, persisted in a `stamps` file next to the tree (`readStamps`/`writeStamps`, `stale.go:171`/`stale.go:188`). Unchanged stamp → skip; changed or gone → re-hash. A corrupt/missing stamps file just means everything gets re-hashed once.

**2. Content check — hash, re-anchor, or mark stale** (`internal/know/stale.go:71` `CheckFile`)
Each citation (`RefFile`, ref shaped like `path:12-40`) carries a hash of its cited lines, computed per-line (FNV-1a) and combined as a polynomial mod 2³² (`internal/know/hash.go`). For a changed file, `CheckFile` decides three ways:
- **Unchanged**: the range at the recorded lines still hashes equal → nothing done.
- **Moved within the file**: it rolls that polynomial hash down the whole file in O(len) with a sliding window (`Roll`, `hash.go:61`), preferring the match nearest the old position; a hit calls `Reanchor` to point the evidence at the new lines — counted as "moved", the node stays current.
- **Genuinely changed**: no match (or the file is gone) → the node gets `FlagStale`.

**3. Propagation**
- `MarkSuspect` (`stale.go:118`) walks *reverse* `requires` and `derived-from` edges breadth-first from the stale nodes and flags everything resting on them `FlagSuspect`.
- `FlagCycles` also runs in the same pass, keeping one question node per open requires-loop.

`refreshKnow` then returns a summary ("N stale, M suspect, K references followed moved code") for the session.

Two caveats worth noting: the content check only re-anchors *within the same path* — if a file is renamed/moved to a new path, the old path is "gone", so its nodes go stale rather than being followed to the new location. And a citation that shrinks or grows in line count can't match the fixed-length rolling window either, so it degrades to stale.

## calls
list_directory map[depth:3 path:internal/know]
semantic_search map[query:evidence staleness detection changed moved file path]
read_file map[path:internal/know/stale.go]
find_callers map[name:CitedFiles]
read_file map[path:internal/know/hash.go]
read_file map[path:internal/think/stale.go]
find_callers map[name:refreshKnow]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519947413"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:38 internal/know/stale.go:28 stale.go:156 stale.go:171 stale.go:188 internal/know/stale.go:71 hash.go:61 stale.go:118"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

