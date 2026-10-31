Which file pins the advisor's tool list, and what command regenerates it?

right true, 20310 prompt (18944 cached), 375 completion, $0.00190, 3 rounds, 2 tools

## answer


The advisor's tool list is pinned by the golden file `internal/agent/testdata/definitions.advisor.json`, checked byte-for-byte by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:24-80`. There's one golden file per role (`definitions.json`, plus `definitions.minion`/`copilot`/`advisor.json`), because an accidental description or schema change would alter model behavior and invalidate the server prompt cache.

Regenerate it deliberately with:

```
go test ./internal/agent -run Golden -update
```

The list itself comes from the static spec table built once by `defsFor()` (`internal/agent/tools.go:147`) in fixed `toolID` order, so regeneration is deterministic. [n3]

## calls
know_find map[query:advisor tool list pinned regenerate]
know_find map[ids:[n3 n5]]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106339253"
author a2 armin session "bench-1793473478106339253"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/definitions-stability fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned byte-for-byte by the golden test TestDefinitionsGolden (internal/agent/definitions_test.go), with one golden file in testdata/ per role (definitions.json plus definitions.minion/copilot/advisor.json), because an accidental description/schema change would alter model behavior and invalidate the server prompt cache. They are deterministic: defsFor() (internal/agent/tools.go:147) builds each role's list once via sync.Once, in fixed toolID order, from the static spec table. Regenerate deliberately with: go test ./internal/agent -run Golden -update."
  evidence file "internal/agent/definitions_test.go:24-80" 04c0f17e
  evidence file "internal/agent/tools.go:142-163" 6e6e631f
  evidence file "internal/agent/role.type.go:23-31" 3c17162e

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:24 definitions_test.go:62 internal/agent/tools.go:147 definitions_test.go:18-20"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence file "internal/agent/role.go:1-40" 75f9a198
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:135-175" 83eb99cb

