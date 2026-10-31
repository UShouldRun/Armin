How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 71727 prompt (28864 cached), 1095 completion, $0.01006, 6 rounds, 8 tools

## answer


It's a two-layer system: **think** detects changed files cheaply, **know** re-verifies the cited ranges.

**1. Detection — stamp comparison (no VCS needed)**

At session start, `refreshKnow` (internal/think/stale.go:29) is called when the tree opens:

- `g.CitedFiles()` (internal/know/stale.go:26) walks every piece of evidence, keeping only *live* `RefFile` evidence, and maps each cleaned path (parsed from `"path:12-40"` refs by `ParseRange`, stale.go:40) to its evidence IDs.
- Each cited file is `stat`ed (`stampOf`, think/stale.go:90): size, mtime, ctime, inode — all four.
- Those are compared against the last check's stamps, persisted in a binary `stamps` file beside the tree (`readStamps`/`writeStamps`, written via tmp+rename). Only files whose stamp differs — or that are gone — get re-hashed. So steady-state cost is O(cited file stats), regardless of whether the project uses git, hg, jj, svn, fossil, or nothing.

**2. Meaning — re-hashing the cited range (`CheckFile`, internal/know/stale.go:72)**

Evidence stores a hash of the lines it cites. Per evidence in a changed file, three outcomes:

- **Unchanged** — the range `data[start-1:end]` still hashes to `e.Hash` → nothing happens.
- **Moved within the file** — `Roll` (internal/know/hash.go:64) slides a same-length window down the file in O(lines) using a rolling polynomial hash (FNV-1a per line, combined mod 2³² with odd base 1,000,003 so the window update is a multiply-add; tie-breaking prefers the match nearest the old location). When it finds the window, the evidence is `Reanchor`ed (internal/know/graph.go:463) to the new `path:start-end` — silently following moved code.
- **Stale** — file deleted, lines edited, or code moved to a *different* file (Roll only searches within the same file): the evidence's node gets `FlagStale` (internal/know/graph.type.go:48).

**3. Propagation**

- `MarkSuspect` (internal/know/stale.go:103) BFS over *reverse* `requires` and `derived-from` edges from the stale nodes, setting `FlagSuspect` on every live node that rests on them, directly or transitively — so conclusions built on changed evidence get flagged for re-checking too.
- `FlagCycles` (stale.go:131) then refreshes the `cycles/` question node per open `requires` loop.

Finally it writes the new stamps back and reports a summary like "knowledge tree: 2 stale, 5 suspect, 3 references followed moved code".

The design note at internal/know/stale.go:2 makes the split explicit: think finds which files changed and reads them; know decides unchanged / re-anchored / stale and marks the graph.

## calls
search_code map[path:internal/know pattern:evidence]
list_directory map[path:internal/know]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
search_code map[pattern:CheckFile|CitedFiles|MarkSuspect]
find_definition map[name:Reanchor]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know pattern:func .*Reanchor]

## tree after

