How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 68714 prompt (61568 cached), 1296 completion, $0.00674, 8 rounds, 12 tools

## answer


It's a two-layer check: a cheap file-stamp layer in `internal/think`, then content hashing in `internal/know`.

**1. Detecting changed files (without git)** — `internal/think/stale.go:31` `refreshKnow`
- Runs at session start (`internal/think/think.go:265`) after opening the tree.
- `g.CitedFiles()` (`internal/know/stale.go:24`) maps every file referenced by live `RefFile` evidence (refs look like `"path:12-40"`, parsed by `ParseRange`) to its evidence IDs.
- Each cited file gets a stamp: size, mtime, ctime, inode via `unix.Stat` (`stampOf`, `internal/think/stale.go:110`). Last check's stamps live in a `stamps` file beside the tree (`readStamps`/`writeStamps`, uvarint count + path + four little-endian uint64s, written via tmp+rename).
- Only files whose stamp changed — or that are now missing — are re-read and handed to `g.CheckFile`. A missing/corrupt stamps file just means everything gets hashed once.

**2. Per-file content check** — `internal/know/stale.go:59` `CheckFile`
- Line hashing (`internal/know/hash.go`): each line is FNV-1a; a range's hash is a polynomial `H = h₁·Bⁿ⁻¹ + … + hₙ` mod 2³² (base 1000003), stored on the evidence at cite time.
- For each citing evidence:
  - **Unchanged**: current lines hash to the same value → nothing happens.
  - **Moved**: the hash differs, but `Roll` (`hash.go:65`) rolls the polynomial window in O(lines) to find where the same-length block now sits (preferring the match nearest the old position) → `g.Reanchor` updates the evidence's ref and it counts as "moved".
  - **Stale**: no matching block (or the file is gone) → the node is flagged `FlagStale`.

**3. Propagation** — `internal/know/stale.go:103` `MarkSuspect`
- Breadth-first over reverse `requires` and `derived-from` edges from the stale nodes: everything that directly or transitively rests on stale code is flagged `FlagSuspect` (nodes already stale/suspect aren't double-counted).

**4. Cycle pass** — `FlagCycles` keeps one question node per open `requires` loop under `cycles/`, adding new ones and deleting question nodes whose loop no longer exists.

The result is reported back, e.g. `"knowledge tree: 2 stale, 5 suspect, 1 references followed moved code"` (`internal/think/stale.go:97`).

So: stat-based short-circuit for speed, per-line polynomial hashing to distinguish "edited" from "just moved", graph traversal to flag everything built on top.

## calls
list_directory map[depth:3 path:internal/know]
find_todo_fixme map[path:internal/know]
outline_file map[path:internal/know/stale.go]
outline_file map[path:internal/know/hash.go]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CitedFiles]
find_callers map[name:CheckFile]
find_callers map[name:MarkSuspect]
read_file map[path:internal/think/stale.go]
find_callers map[name:refreshKnow]
read_file map[end_line:290 path:internal/think/think.go start_line:240]

## tree after

