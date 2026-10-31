Which file pins the advisor's tool list, and what command regenerates it?

right true, 27133 prompt (23744 cached), 453 completion, $0.00260, 4 rounds, 3 tools

## answer


The advisor's tool list is pinned in **`internal/agent/testdata/definitions.advisor.json`** — part of the golden files checked by `TestDefinitionsGolden` (`internal/agent/definitions_test.go:21`), which keeps each role's marshaled tool definitions byte-stable.

Regenerate it with:

```
go test ./internal/agent -run Golden -update
```

This rewrites all role goldens (`definitions.json` plus `definitions.{minion,copilot,advisor}.json`). Be careful: an accidental change here breaks model behavior and the prompt cache, so only do it when the tool definitions intentionally change.

## calls
know_find map[query:advisor tool list pinned file regenerate command]
know_find map[ids:[n2 n3]]
file_stat map[path:internal/agent/testdata/definitions.advisor.json]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245533587"
author a2 armin session "bench-1793473527245533587"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/definition-stability fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Armin keeps per-role tool definitions byte-stable two ways: a Loop caches one roleDefs per role built once via sync.Once (tools.go:147), and TestDefinitionsGolden (definitions_test.go:21) goldens the marshaled JSON per role — definitions.json plus definitions.{minion,copilot,advisor}.json in testdata/. Accidental changes break model behavior and the prompt cache; regenerate with `go test ./internal/agent -run Golden -update`."
  evidence file "internal/agent/definitions_test.go:21-80" 1c8503e4
  evidence file "internal/agent/tools.go:142-149" 9210a987
  evidence file "internal/agent/role.type.go:23-31" 3c17162e

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go internal/agent/role.type.go"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000

