Which file pins the advisor's tool list, and what command regenerates it?

right true, 56901 prompt (49152 cached), 711 completion, $0.00775, 6 rounds, 7 tools

## answer
The advisor's tool list is pinned in `internal/agent/testdata/definitions.advisor.json` — one golden file per role (`definitions.minion.json`, `definitions.copilot.json`, plus `definitions.json` for the full table).

The test `TestDefinitionsGolden` in `internal/agent/definitions_test.go:25` marshals each role's `l.Definitions()` and compares it byte-for-byte against the golden file (a mismatch writes a `.got` file for diffing). To regenerate after an intentional change, run:

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (`definitions_test.go:17`) rewrites the `testdata/definitions*.json` files. This is deliberate: any description or schema drift changes what the model is told and would throw away prompt caches.

## calls
search_code map[pattern:regenerat.*tool|tool.*list|tools\.txt|tools\.go]
semantic_search map[query:advisor tool list pinned generated]
find_files map[pattern:definitions.*.json]
search_code map[path:internal/agent pattern:-update]
search_code map[path:internal/agent pattern:update.*golden|goldenFile|definitions\.]
read_file map[end_line:110 path:internal/agent/definitions_test.go start_line:60]
read_file map[end_line:45 path:internal/agent/definitions_test.go start_line:1]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474539444737731"
author a2 armin session "bench-1793474539444737731"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/golden-definitions fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned byte-for-byte by the golden test TestDefinitionsGolden (internal/agent/definitions_test.go): one golden file per role (definitions.{minion,copilot,advisor}.json + all-tools definitions.json) in internal/agent/testdata/. Any byte change fails the test and writes a .got file for diffing. Regenerate intentionally with go test ./internal/agent -run Golden -update. Reason: wording changes alter model behavior and invalidate the prompt cache; role lists are pinned because switching roles re-sends the tool block."
  evidence file "internal/agent/definitions_test.go:22-80" 2eb3c6bd
  evidence session "internal/agent/testdata/" 00000000

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go"
  evidence session "internal/agent/definitions_test.go" 00000000

