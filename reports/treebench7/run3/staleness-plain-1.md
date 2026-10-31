How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 82498 prompt (73088 cached), 1134 completion, $0.00733, 8 rounds, 11 tools

## answer


The knowledge tree (`internal/know`) keeps a hash of every range of code each piece of file evidence cites, and the staleness check runs at session start. The mechanics split between two packages:

**What's stored** (`internal/know/hash.go`)
- Each cited line is hashed individually with FNV-1a (`LineHash`), and a range's hash is a polynomial over its line hashes mod 2³² (`RangeHash`).
- Because it's a polynomial, `Roll` can slide a window of the original length down the file in O(len) and find where the same lines moved to. The hash is only ever compared against the same evidence's earlier value, never used as an identity.

**Who triggers it** (`internal/think/stale.go:34`, called from `think.go:273` at session start)
- `refreshKnow` needs no version control: it stats each cited file and compares a "stamp" — size, inode, mtime, ctime — against the one saved in a `stamps` file beside the tree. Only files whose stamp changed (or that vanished) get read and re-hashed. So cost is O(cited file stats) most sessions, and O(changed lines) only when something actually changed.
- A missing/corrupt stamps file just means everything gets re-hashed once.

**What happens per changed file** (`internal/know/stale.go:60`, `CheckFile`)
1. If the cited range still hashes to the stored value → fine, do nothing.
2. Else `Roll` searches the file for a same-length window with the same hash, preferring the match nearest the old position (repeated code can match several places). If found, `Reanchor` (`graph.go:463`) updates the evidence's ref to `path:newstart-newend` — the citation is silently followed to its new location and counted as "moved".
3. If the lines are gone, changed, or the file is deleted → the evidence's node gets `FlagStale`.

**Ripple effect** (same file)
- `MarkSuspect` does a breadth-first walk up the reverse `requires` and `derived-from` edges, flagging `FlagSuspect` every live node that rests on a stale one.
- `FlagCycles` then reconciles open requires-loops with `cycles/` question nodes.

The result is surfaced as a one-liner like "knowledge tree: 1 stale, 3 suspect, 2 references followed moved code".

## calls
list_directory map[path:internal/know]
semantic_search map[query:detect that cited code evidence changed or moved file]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:CitedFiles]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/think pattern:refreshKnow]
search_code map[path:internal/know pattern:func (g \*Graph) Reanchor]
search_code map[path:internal/know pattern:Reanchor]
read_file map[end_line:490 path:internal/know/graph.go start_line:455]

## tree after

