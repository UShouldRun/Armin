How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 81874 prompt (72896 cached), 1362 completion, $0.00772, 7 rounds, 12 tools

## answer


It's a two-layer mechanism: cheap stamps to detect *which* cited files changed, and content hashes to detect *what* happened to each cited range.

**1. Session start — `refreshKnow` (internal/think/stale.go:27)**

Called once per session when the think loop opens (internal/think/think.go:265), with no version control involved:

- `g.CitedFiles()` (internal/know/stale.go:24) collects every live file evidence, mapping each cited path (`path:12-40` form, parsed by `ParseRange`) to its evidence IDs.
- Each file's **stamp** — size, mtime, ctime, inode (internal/think/stale.type.go:9) — is compared against the previous check's stamps, stored in a `stamps` file beside the tree (uvarint count, then per-file path + four little-endian values). Inode/ctime are there so a *replaced* file is caught even if size+mtime match.
- Only stamped-changed or gone files are read and re-checked. If the stamps file is missing/corrupt, everything is re-hashed once. Stamps are rewritten atomically (tmp + rename) after the check.

**2. Per file — `CheckFile` (internal/know/stale.go:59)**

Each evidence stores a hash of the exact lines it cites when written: per-line FNV-1a, combined into a 32-bit polynomial over the range (internal/know/hash.go). For a changed file it compares against the current content:

- **Unchanged** — the same range still hashes the same: done, the node keeps its `current` status (the one-line output shows `current` only for nodes whose file evidence passed this check, internal/know/line.go:67).
- **Moved within the file** — the hash didn't match at the old spot, but `Roll` (internal/know/hash.go:55) slides the n-line window down the file in O(1) per line using the polynomial structure, preferring the match nearest the old position. When found, `Reanchor` (internal/know/graph.go:463) rewrites the evidence's range to the new lines — a logged `opReanchor` op, so the citation follows the code.
- **Gone / file deleted / lines changed** — the node gets `FlagStale` (it then prints as `STALE` and never `current`).
- **Downstream** — `MarkSuspect` (internal/know/stale.go:112) then does a breadth-first walk over *reverse* `requires` and `derived-from` edges, flagging `FlagSuspect` everything resting on a stale node that isn't already stale/suspect.

The result is reported to the session as e.g. "knowledge tree: 2 stale, 5 suspect, 1 references followed moved code" (internal/think/stale.go:88-101). Separately, `FlagCycles` handles the unrelated case of open `requires` loops by keeping one question node per loop under `cycles/`.

## calls
list_directory map[depth:3 path:internal/know]
search_code map[path:internal/know pattern:STALE]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:CitedFiles]
search_code map[path:internal/know pattern:Reanchor]
read_file map[path:internal/think/stale.go]
find_callers map[name:refreshKnow]
read_file map[end_line:495 path:internal/know/graph.go start_line:455]
read_file map[end_line:285 path:internal/think/think.go start_line:245]
search_code map[path:internal/think pattern:type stamp]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473478108096017"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:27 internal/think/think.go:265 internal/know/stale.go:24 internal/think/stale.type.go:9 internal/know/stale.go:59 internal/know/line.go:67 internal/know/hash.go:55 internal/know/graph.go:463 internal/know/stale.go:112 internal/think/stale.go:88-101"
  evidence file "internal/know/graph.go:455-495" 4fc1337c
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000
  evidence file "internal/think/think.go:245-285" 55878440

