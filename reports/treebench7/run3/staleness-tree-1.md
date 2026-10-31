How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 68066 prompt (61376 cached), 1309 completion, $0.00669, 7 rounds, 12 tools

## answer


The mechanism is split across two files: `internal/think/stale.go` (session-start driver) and `internal/know/stale.go` + `hash.go` (the graph-side checks).

**When it runs:** at session start, after the tree is opened (`internal/think/think.go:273` calls `refreshKnow`). Skipped for read-only trees (`internal/think/stale.go:28`).

**Step 1 — cheap change detection, no VCS needed.** `refreshKnow` collects every file cited by live file evidence via `g.CitedFiles()` (`internal/know/stale.go:23`), which maps each cleaned path to its `EvidenceID`s. For each cited file it stats the file and compares a *stamp* — size, mtime, ctime, inode (`stampOf`, `internal/think/stale.go:116`) — against stamps persisted from the last check in a `stamps` file beside the tree (`readStamps`/`writeStamps`, `internal/think/stale.go:133-181`). A missing/corrupt stamps file means everything gets re-hashed once. So unchanged files are skipped in O(cited files) stats; only changed or gone files are read.

**Step 2 — re-anchoring vs. staleness.** For each changed file, `g.CheckFile` (`internal/know/stale.go:52`):
- If the cited range `path:start-end` still hashes identically (`RangeHash` of the line hashes), the evidence is untouched — the file changed but not the cited lines.
- Otherwise `Roll` (`internal/know/hash.go:60`) slides the rolling hash across the whole file in O(lines) looking for a window of the same length with the same hash. If found (preferring the spot nearest the old start, so repeated code blocks don't mis-locate), the reference *moved*: `g.Reanchor` rewrites it to the new `path:newstart-newend`.
- If no match, or the file is gone, the evidence's node is flagged `FlagStale` (idempotent — already-stale nodes aren't re-reported).

**Step 3 — propagate suspicion.** `MarkSuspect` (`internal/know/stale.go:101`) does a BFS over *reverse* `requires` and `derived-from` edges from every newly-stale node, flagging every live node that rests on it as `FlagSuspect`. That's why you see the hierarchy: stale = "my own evidence no longer matches", suspect = "something I depend on is stale".

**Step 4 — `FlagCycles`** then reconciles `requires` loops into question nodes under `cycles/`, unrelated to file changes but part of the same pass.

Finally, new stamps are written and the user is told a summary line like "knowledge tree: N stale, M suspect, K references followed moved code" — or nothing if nothing changed. In the prompt header, `current` means it passed today's check, STALE/SUSPECT mean re-read the `at:` ref.

## calls
list_directory map[depth:3 path:internal/know]
semantic_search map[query:evidence staleness check when cited code changed or moved]
read_file map[path:internal/know/stale.go]
find_callers map[name:CitedFiles]
find_callers map[name:CheckFile]
outline_file map[path:internal/know/hash.go]
read_file map[path:internal/think/stale.go]
search_code map[path:internal/know/hash.go pattern:func (RangeHash|LineHashes|Roll)]
find_callers map[name:refreshKnow]
search_code map[path:internal/know/hash.go pattern:func Roll]
read_file map[end_line:290 path:internal/think/think.go start_line:255]
read_file map[end_line:90 path:internal/know/hash.go start_line:55]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474539444632263"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/think.go:273 internal/think/stale.go:28 internal/know/stale.go:23 internal/think/stale.go:116 internal/think/stale.go:133-181 internal/know/stale.go:52 internal/know/hash.go:60 internal/know/stale.go:101"
  evidence file "internal/know/hash.go:55-90" f428fb96
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000
  evidence file "internal/think/think.go:255-290" 25aa569f

