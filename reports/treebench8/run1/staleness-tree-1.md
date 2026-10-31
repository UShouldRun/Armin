How does the knowledge tree (internal/know) notice that code cited as evidence changed or moved?

right true, 135839 prompt (115136 cached), 2055 completion, $0.00714, 9 rounds, 13 tools

## answer



















`internal/know` doesn't watch files itself — it's handed the changed content and decides what it means. The whole flow:

**1. Who changed: `think.refreshKnow`** (`internal/think/stale.go:34`, called at session start from `internal/think/think.go:273`).
- `CitedFiles()` (`internal/know/stale.go:26`) maps each file that live file evidence points at → its evidence.
- For each, `stampOf` stats the file (size, mtime, ctime, inode) and compares against the last check's stamps in a `stamps` file beside the tree (`stale.go:141-216`). Only files whose stamp differs, or that are gone, are re-read — no VCS, no watching.

**2. What the change means: `Graph.CheckFile`** (`internal/know/stale.go:72`). For each cited range of a changed file it compares hashes and gets one of three outcomes:
- **Unchanged** — the range's lines still hash to the same value as stored on the evidence (`stale.go:95`): skip.
- **Moved within the file** — the exact range no longer matches, but `Roll` (`internal/know/hash.go:61`) slides a same-length window down the whole file in O(1) per line looking for a matching range hash; when found (preferring the match nearest the old start), the evidence is `Reanchor`'d to the new `path:start-end` (`stale.go:101`). Counted as moved, not stale.
- **Stale** — no same-length window matches (file deleted or the code really changed), so the citing node gets `FlagStale` (`stale.go:114`).

**Why hashing works this way:** each line is FNV-1a hashed separately; a range hash is a polynomial over its line hashes mod 2³² with an odd base (`hash.go:8-39`), so a fixed-length window can be rolled O(file-length) to find moved code. The hash is only ever compared against its own earlier value — never used as an identity.

**3. Consequences:** `MarkSuspect` (`stale.go:128`) breadth-first marks every live node that `requires` or was `derived-from` a stale node as suspect; `FlagCycles` keeps one question node per open requires loop. Results are shown to the model as e.g. "knowledge tree: 2 stale, 3 suspect, 1 references followed moved code", and the short tree lines then print `STALE`/`SUSPECT` or `current` accordingly (`internal/know/line.go:81`).

## calls
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:stale|STALE]
read_file map[path:internal/know/stale.go]
read_file map[path:internal/know/hash.go]
find_callers map[name:CheckFile]
find_callers map[name:CitedFiles]
find_callers map[name:Reanchor]
read_file map[path:internal/think/stale.go]
search_code map[pattern:refreshKnow]
find_definition map[name:Reanchor]
search_code map[path:internal pattern:refreshKnow|func \(g \*Graph\) Reanchor]
know_write map[claim:The knowledge tree notices changed/moved cited code at session start via think.refreshKnow (internal/think/thinal → stale.go:34)…
know_write map[claim:know.CheckFile (internal/know/stale.go:72) classifies each cited range of a changed file into three outcomes: unchanged (the range…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793475801300389026"
author a2 armin session "bench-1793475801300389026"

n1 know topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 know/staleness topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 know/staleness/detection fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "The knowledge tree notices changed/moved cited code at session start via think.refreshKnow (internal/think/thinal → stale.go:34): it stats each file cited by file evidence and compares its stamp (size, mtime, ctime, inode, from a \"stamps\" file beside the tree) with the last check, so only changed/gone files get re-hashed; VCS-free. A missing/damaged stamps file just re-hashes everything once."
  evidence file "internal/think/stale.go:34-125" 5d4162d4
  evidence file "internal/think/think.go:273" 6a1830f1

n4 know/staleness/classification fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "know.CheckFile (internal/know/stale.go:72) classifies each cited range of a changed file into three outcomes: unchanged (the range still hashes the same), moved within the file (a same-length window elsewhere in the file rolls to the same range hash via Roll, so the evidence is Reanchor'ed to the new lines), or stale (nothing matches, so the citing node gets FlagStale). Range hashes are polynomial over per-line FNV-1a hashes mod 2^32 (internal/know/hash.go), enabling O(file) rolling-window search. Stale nodes then propagate FlagSuspect breadth-first to everything resting on them via reverse requires/derived-from edges (MarkSuspect, stale.go:128)."
  derived-from n3
  evidence file "internal/know/stale.go:72-157" 0bca12c6
  evidence file "internal/know/hash.go:1-92" 3ca872f4
  evidence file "internal/know/graph.go:463" bcbe4639

n5 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n6 sessions/how-does-the-knowledge file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:34 internal/think/think.go:273 internal/know/stale.go:26 stale.go:141-216 internal/know/stale.go:72 stale.go:95 internal/know/hash.go:61 stale.go:101 stale.go:114 hash.go:8-39 stale.go:128 internal/know/line.go:81"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

