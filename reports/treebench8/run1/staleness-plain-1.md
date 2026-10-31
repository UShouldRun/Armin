How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 81224 prompt (70400 cached), 1418 completion, $0.00461, 7 rounds, 10 tools

## answer


















It's a two-layer mechanism: `internal/think` decides *which* files to check, and `internal/know` re-hashes the cited ranges and updates the graph.

**1. Detecting changed files — file stamps, no VCS** (`internal/think/stale.go:1-15`)

At session start, `refreshKnow` (called from `internal/think/think.go:273` when the tree is opened) works without any version control:

- `g.CitedFiles()` (`internal/know/stale.go:22`) maps every file cited by live `RefFile` evidence (refs of the form `path:12-40`) to the evidence pointing at it.
- Each cited file is `stat`'d and its stamp — size, inode, mtime, ctime (`stampOf`, `internal/think/stale.go:152`) — is compared with the one saved in a `stamps` file stored beside the tree (uvarint-encoded, written atomically via temp+rename). That's a constant O(cited files) per session.
- Only files whose stamp changed, or that are gone, get read and passed to `CheckFile`. If the stamps file is missing or corrupt, everything is re-hashed once.

**2. Verifying the cited code — content hashes** (`internal/know/stale.go:72`, `internal/know/hash.go`)

Evidence doesn't store line numbers as the source of truth — it stores a hash of the lines it cites: each line is FNV-1a hashed, and the range hash is a polynomial over those line hashes mod 2³² (`hash.go:1-9`). That makes same-length windows rollable:

For each piece of evidence on a changed file, `CheckFile` re-hashes:
- **Unchanged** — `RangeHash` of the old range still equals the stored hash → untouched.
- **Moved within the file** — `Roll` (`hash.go:63`) slides the n-line window down the file in O(len) rolling-hash time, preferring the match nearest the old position (since repeated code can match in several places). When it finds one, `g.Reanchor` (`internal/know/graph.go:463`) updates the evidence's ref to the new `path:at+1-at+n`, and the node stays alive.
- **Truly changed or deleted** (file gone, or no matching window) → the node gets `FlagStale` via `SetFlags`.

**3. Propagating the damage** (back in `refreshKnow`)

- `MarkSuspect` (`internal/know/stale.go:128`) walks breadth-first over *reverse* `requires` and `derived-from` edges from the stale nodes — everything that rests on a stale node gets `FlagSuspect` (unless already stale/suspect).
- `FlagCycles` (`internal/know/stale.go:193`) maintains one question node per open `requires` loop under a `cycles/` directory, deleting question nodes whose loop got resolved.
- Finally, stamps for the new file states are written back, and a summary line is surfaced: "knowledge tree: 3 stale, 5 suspect, 2 references followed moved code…".

So the short answer: **inode/mtime/size/ctime stamps detect that a cited file changed cheaply; a rolling polynomial hash over per-line FNV-1a hashes then distinguishes "still the same lines" from "the lines moved elsewhere in the file (re-anchor)" from "actually changed (mark stale)", and a BFS over reverse dependency edges flags everything downstream as suspect.** The consumer-facing side (`internal/know/line.go:67`) only ever shows a file evidence as *current* when its node carries neither `FlagStale` nor `FlagSuspect`.

One caveat: cross-file moves/renames aren't followed — `Roll` only searches within the same file; a ref to a file that was renamed/removed simply goes stale.

## calls
list_directory map[depth:3 path:internal/know]
search_code map[path:internal/know pattern:stale|invalid|drift|moved|renamed]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:Reanchor]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know pattern:func \(g \*Graph\) Reanchor]
find_callers map[name:refreshKnow]
read_file map[end_line:285 path:internal/think/think.go start_line:255]

## tree after

