Which file pins the advisor's tool list, and what command regenerates it?

right true, 25181 prompt (18944 cached), 465 completion, $0.00281, 3 rounds, 3 tools

## answer


The advisor's tool list is pinned by the golden file `internal/agent/testdata/definitions.advisor.json` — `internal/agent/definitions_test.go` (the `Golden` test) byte-exact compares the marshaled `Loop.Definitions()` for each role against its golden file ([n2]).

Regenerate it with:

```
go test ./internal/agent -run Golden -update
```

On mismatch the test writes `definitions.advisor.json.got` next to it so you can diff; run the above to overwrite it deliberately. Note the role's eligibility itself is decided by the `roles` mask in each tool spec (e.g. `internal/agent/role.go`'s `allows`), so changing a role mask and regenerating is how the pinned list is kept in sync.

## calls
know_find map[query:advisor tool list pinned regenerate]
search_code map[pattern:advisor]
know_find map[ids:[n2]]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473424974534498"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-make file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:26 definitions_test.go:67 tools.go:141-145 tools.go:143-155"
  evidence session "internal/agent/definitions_test.go" 00000000

