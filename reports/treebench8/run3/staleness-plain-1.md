How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 68018 prompt (55552 cached), 1215 completion, $0.00398, 7 rounds, 9 tools

## answer

















It works in two layers: `internal/think` (the "think" session, at start) decides which files to re-check, and `internal/know` does the actual hash comparison and bookkeeping.

**1. Cheap change detection without VCS — `refreshKnow`** (internal/think/stale.go:34)
- `g.CitedFiles()` (internal/know/stale.go:29) maps every file cited by live `RefFile` evidence (`"path:12-40"`) to the evidence that cites it.
- For each cited file it takes a "stamp" — size, inode, mtime, ctime via `unix.Stat` (`stampOf`, stale.go:163) — and compares it against stamps saved at the last check in a `stamps` file beside the tree. Only files whose stamp changed (or that disappeared) get read and re-hashed; the rest are skipped. This works with git/hg/jj/none alike, in O(cited files).

**2. Per-file classification — `Graph.CheckFile`** (internal/know/stale.go:69)
For each cited range, the outcome is one of three:
- **Unchanged**: the current lines at `start:end` produce the same stored hash (`RangeHash(lines[start-1:end]) == e.Hash`).
- **Moved**: the range no longer matches there, so `Roll` (internal/know/hash.go:59) slides a same-length window down the file in O(file length), rolling the hash in O(1) per line, looking for a window with the old hash — preferring the match closest to the old position (repeated code can match several places). A hit calls `Reanchor` (internal/know/graph.go:463) to point the evidence at `path:at-at+n`; the node stays live.
- **Stale**: no matching window (code edited, file truncated, or file gone) → the evidence's node gets `FlagStale`.

The hashing scheme (hash.go:1-12): each line is FNV-1a hashed individually, and a range's hash is a polynomial over line hashes mod 2³² — which is exactly what makes the rolling search possible. Hashes are only ever compared against the same evidence's stored value, never used as identity.

**3. Propagation** (internal/know/stale.go:125)
`MarkSuspect` then does a BFS over *reverse* `requires` and `derived-from` edges from every newly stale node, flagging every live node that rests on it (directly or transitively) as `FlagSuspect`. Finally `FlagCycles` keeps one question node per open requires-loop under a `cycles/` directory. The session prints a summary like "knowledge tree: 3 stale, 5 suspect, 2 references followed moved code".

One caveat: a *rename/move of the whole file* isn't followed — the stamp under the old path disappears, the file counts as "gone", and its evidence's nodes go stale. Move-detection works for code that moved *within* a file only.

## calls
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:invalidat]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:CitedFiles]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know pattern:func (g \*Graph) Reanchor]
search_code map[pattern:func.*Reanchor]

## tree after

