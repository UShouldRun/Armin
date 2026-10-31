How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 52225 prompt (43584 cached), 1124 completion, $0.00341, 5 rounds, 8 tools

## answer














It's a two-layer design: `internal/think` decides *which files changed* (without VCS), and `internal/know` decides *what to do* about each cited range.

**1. Detecting changed files — `internal/think/stale.go`**

At session start, `refreshKnow` (think/stale.go:40) stats every file the tree cites and compares against a stamp store:

- `Graph.CitedFiles()` (know/stale.go:25) collects all live file evidence (`RefFile` refs, i.e. `"path:12-40"` strings) grouped by path.
- For each cited file, a `stamp` of size, mtime, ctime, and inode (think/stale.go:154) is stat'd and compared to the last check's stamps, persisted in a small `stamps` file next to the tree (uvarint-prefixed paths + 4 little-endian u64s, written via temp+rename). This is deliberately VCS-agnostic — no git needed, just O(cited files) stats.
- Files whose stamp matches are skipped entirely; changed or deleted files get read and passed to `CheckFile`.

**2. Per-range verdict — `know.CheckFile` (know/stale.go:72)**

Each evidence stores the hash of the lines it cited. The hash (know/hash.go) is a polynomial over per-line FNV-1a hashes mod 2³²:

- **Unchanged**: the cited range still hashes to the stored value → untouched.
- **Moved**: if the range hash doesn't match, `Roll` (know/hash.go:67) slides an n-line window (n = the range's line count) down the file in O(lines), using the polynomial's invertible base to update the hash in O(1) per step, and picks the match nearest the old position (since repeated code can match several places). A match → `Reanchor` (know/graph.go:463) rewrites the evidence's ref to the new lines; the node is *not* marked stale.
- **Gone or truly changed** (file deleted, or no same-length window matches): the node gets `FlagStale`.

**3. Propagation**

- `MarkSuspect` (know/stale.go:128) does a BFS over reverse `requires` and `derived-from` edges from the stale nodes, flagging everything that transitively rests on them with `FlagSuspect` — so knowledge built *on* changed code is downgraded, not deleted.
- `FlagCycles` runs a separate pass recording open `requires` loops as question nodes under a `cycles/` topic.
- Finally `refreshKnow` writes the new stamps and returns a one-line summary like "knowledge tree: 2 stale, 5 suspect, 1 references followed moved code".

So: cheap stat-based change detection → line-hash re-verification per cited range → re-anchoring for moves, `stale` for real edits/deletions, `suspect` for everything depending on them.

## calls
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:stale|drift|invalidate|verify]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CitedFiles]
find_definition map[name:Reanchor]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know pattern:func \(g \*Graph\) Reanchor]

## tree after

