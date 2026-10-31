How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 73169 prompt (58816 cached), 1269 completion, $0.00763, 7 rounds, 9 tools

## answer


Now let me see how the caller drives this — who finds the changed files:



The knowledge tree detects changes to cited code in two layers:

## 1. The caller (`internal/think/stale.go` — `refreshKnow`)

Runs once at session start (`internal/think/think.go:273`), with no version control needed:

- **CitedFiles** (`internal/know/stale.go:26`) maps every live file-evidence ref (`"path:12-40"`, parsed by `ParseRange`) to the evidence IDs that point at it.
- **Stamp comparison** — each cited file gets a `stamp` of size, mtime, ctime, and inode from `unix.Stat` (`stampOf`, `stale.go:169`). Last check's stamps live in a `stamps` file beside the tree (uvarint-prefixed paths + four LE int64s, written atomically via tmp+rename). Only files whose stamp changed — or that are gone — get read and re-hashed; unchanged ones are skipped. A missing/damaged stamp file just re-hashes everything once.
- This is deliberately VCS-agnostic: git/hg/jj/svn/fossil/none all cost the same O(cited files) stats.

## 2. The content check (`internal/know`)

- **Hashing** (`hash.go`): each line gets an FNV-1a hash; a range's hash is a polynomial `h(l₁)·Bⁿ⁻¹ + … + h(lₙ)` mod 2³² over those line hashes. A hash is only ever compared against the same evidence's stored value, never used as identity.
- **CheckFile** (`stale.go:72`) then classifies each cited range of a changed file:
  - **Unchanged**: the lines at the old position still hash the same → nothing done (also the fast exit, so a file that changed elsewhere but not at the cited lines costs one range hash).
  - **Moved within the file**: the old hash differs, but `Roll` slides a same-length, same-hash window across the file in O(1) per line (amortized, using the polynomial's invertible base). If a match is found, the evidence is **re-anchored** to the new location via `Reanchor` (`graph.go:463`); among several equal matches it prefers the one nearest the old position.
  - **Gone / stale**: no matching window (or the file is deleted) → the evidence's node gets `FlagStale`.
- **Propagation**: `MarkSuspect` then walks breadth-first *against* the `requires` and `derived-from` edges — everything that transitively rests on a stale node gets `FlagSuspect` (but not stale ones already marked, and not the stale roots again).
- **`FlagCycles`** is an adjacent hygiene pass: open `requires` loops get a question node under a `cycles/` dir; question nodes whose loop was resolved get deleted.

Finally `refreshKnow` reports a summary like `knowledge tree: 3 stale, 7 suspect, 2 references followed moved code, 1 open requires loop`, or says nothing when everything is clean. So "moved" code is silently re-pointed, "changed" code marks its node stale and suspects its dependents, and "deleted" files mark all their citing nodes stale.

## calls
list_directory map[path:internal/know]
semantic_search map[query:detect evidence code changed or moved]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
search_code map[file_type:go pattern:CitedFiles|CheckFile]
read_file map[path:internal/think/stale.go]
search_code map[file_type:go path:internal/know pattern:func .*Reanchor]
find_callers map[name:refreshKnow]
read_file map[end_line:285 path:internal/think/think.go start_line:255]

## tree after

