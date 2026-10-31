Which file pins the advisor's tool list, and what command regenerates it?

right true, 51389 prompt (39936 cached), 634 completion, $0.00747, 5 rounds, 5 tools

## answer
The advisor's tool list is pinned byte-for-byte in **`internal/agent/testdata/definitions.advisor.json`** by the golden test `TestDefinitionsGolden` in `internal/agent/definitions_test.go:26` — the test fails on any drift, since an accidental change to a description or schema alters model behavior and invalidates prompt caches.

To regenerate it deliberately:

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (`definitions_test.go:18-20`) rewrites the `testdata/definitions*.json` files for each role. The list itself comes from the spec table in `tools.go:51-54` filtered through the advisor role (`Loop.Definitions`, `tools.go:147`).

## calls
read_file map[end_line:180 path:internal/agent/tools.go start_line:130]
search_code map[path:internal/agent pattern:toolUnknown|toolCount|specs\s*=|var specs]
search_code map[path:internal/agent pattern:advisor]
search_code map[pattern:generate.*tools|tools.*generate|pin]
search_code map[path:internal/agent pattern:definitions\.advisor\.json|-update|update.*flag|flag\.Bool]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519132117"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-make file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/loop.type.go:109 internal/agent/tools.go:149-170 internal/agent/policy.type.go:31-33 internal/agent/definitions_test.go:25 definitions_test.go:24-26 definitions_test.go:76-79 definitions_test.go:72-75 internal/agent/loop_test.go:19"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:140-175" c9df97e0

