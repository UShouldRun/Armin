How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 64137 prompt (55232 cached), 1068 completion, $0.00623, 6 rounds, 10 tools

## answer


The mechanism is a **hash-based staleness check with a "stamp" cache**, split between `think` (driver) and `know` (logic):

**1. Detecting that a file changed — `internal/think/stale.go` (`refreshKnow`)**

No git is needed. At session start (`think.go:265`) it stats each cited file and compares a `stamp{Size, Mtime, Ctime, Ino}` (via `unix.Stat`) against the last check's stamps, which persist beside the tree in a `stamps` file (`think/stale.go:48-55`, `readStamps`). Only files whose stamp changed or that vanished are re-read and re-hashed — O(cited files) stats, no version control.

**2. Classifying what happened to the cited code — `know.CheckFile` (`internal/know/stale.go:72`)**

Evidence stores a `path:12-40` ref plus a 32-bit hash of the cited lines. The hash is a polynomial over per-line FNV-1a values (`internal/know/hash.go`), chosen so a same-length window can be **rolled** down the file in O(lines). For each piece of evidence citing the file:

- **Unchanged**: `RangeHash(lines[start-1:end]) == e.Hash` → nothing done.
- **Moved within the file**: `Roll(lines, n, e.Hash, start-1)` slides the hash until it matches the old one, preferring the match nearest the old position (so repeated code lands where it was) → `Reanchor` points the evidence at the new lines (`know/graph.go:463`), counted as "moved".
- **Gone/changed**: no matching window (or the file is deleted) → the node gets `FlagStale` set.

**3. Propagating the doubt — `MarkSuspect` (`stale.go:128`)**

Breadth-first over reverse `requires` and `derived-from` edges: everything resting on a stale node (and not already stale/suspect) gets `FlagSuspect`.

**4. `FlagCycles`** then reconciles the question nodes recorded for open requires loops.

`refreshKnow` finally reports, e.g., "knowledge tree: 2 stale, 5 suspect, 1 references followed moved code". So "moved" code is silently re-anchored, "changed" code marks its node stale and its dependents suspect, and a deleted file stales everything citing it.

## calls
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:stale|changed|moved|invalidat]
read_file map[path:internal/know/stale.go]
find_callers map[name:CheckFile]
read_file map[path:internal/think/stale.go]
outline_file map[path:internal/know/hash.go]
search_code map[path:internal/know/hash.go pattern:^func|^//]
find_callers map[name:refreshKnow]
read_file map[end_line:280 path:internal/think/think.go start_line:245]
search_code map[path:internal/know pattern:func \(g \*Graph\) Reanchor]

## tree after

