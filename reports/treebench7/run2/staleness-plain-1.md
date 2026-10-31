How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 61686 prompt (51520 cached), 1134 completion, $0.00637, 7 rounds, 8 tools

## answer


The check runs at session start via `refreshKnow` (called from `internal/think/think.go:273` right after the tree is opened), and it works in two layers — a cheap OS-level stamp check, then a hash-based content check:

**1. Finding what's cited** — `Graph.CitedFiles()` (internal/know/stale.go:26) collects every live `RefFile` evidence entry, parses its ref with `ParseRange` (format `path:12-40`), and groups evidence IDs by cleaned file path.

**2. Fast skip via file stamps** — `refreshKnow` (internal/think/stale.go:33) stats each cited file with `unix.Stat` and builds a `stamp` of size, mtime, ctime, and inode. Last session's stamps are read from a sidecar file (`<tree dir>/stamps`, uvarint-encoded). Files whose stamp matches are skipped entirely — no reading, no hashing, works with or without git. A missing/corrupt stamps file just forces a full re-hash once.

**3. Content check for changed files** — for each stamp-changed or deleted file, `os.ReadFile` is followed by `Graph.CheckFile` (internal/know/stale.go:54). For each cited range it:
- recomputes `LineHashes` on the file (per-line FNV-style hashes, internal/know/hash.go:39) and compares `RangeHash(lines[start-1:end])` to the hash stored on the evidence — **unchanged** if equal (the file can change elsewhere without touching this range);
- otherwise runs `Roll` (internal/know/hash.go:60), which slides an O(len(lines)) rolling hash over the file looking for a window of the same length `n` with the same hash, preferring the position nearest the old start (repeated code can match several places). If found, the evidence is **re-anchored** via `Graph.Reanchor` (internal/know/graph.go:463) to the new `path:lo-hi` and counted as "moved";
- if no match, the evidence's node gets `FlagStale` set (unless already stale) and is returned as newly stale.

**4. Propagation** — `MarkSuspect` (internal/know/stale.go:97) then does a breadth-first walk over *reverse* `requires` and `derived-from` edges from each stale node, flagging every live node resting on it `FlagSuspect` (with a bitset to avoid revisits). Finally `FlagCycles` maintains the `cycles/` directory of question nodes for open requires loops.

So "changed" = range hash differs with no matching window anywhere in the file → node goes stale and its dependents suspect; "moved" = identical window found elsewhere in the same file → the reference is silently updated. It all ends by rewriting the stamps file (atomic write via temp + rename) and reporting something like "knowledge tree: 2 stale, 5 suspect, 1 references followed moved code".

## calls
list_directory map[depth:2 path:internal/know]
read_file map[path:internal/know/stale.go]
search_code map[path:internal/know pattern:func (LineHashes|RangeHash|Roll)\(|func \(g \*Graph\) Reanchor]
find_callers map[name:CheckFile]
read_file map[path:internal/think/stale.go]
read_file map[end_line:75 path:internal/know/hash.go start_line:20]
find_callers map[name:refreshKnow]
read_file map[end_line:285 path:internal/think/think.go start_line:255]

## tree after

