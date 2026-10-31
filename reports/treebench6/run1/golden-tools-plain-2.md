Which file pins the advisor's tool list, and what command regenerates it?

right true, 36344 prompt (23872 cached), 431 completion, $0.00393, 4 rounds, 5 tools

## answer


The advisor's tool list is pinned in **`internal/agent/testdata/definitions.advisor.json`** — a byte-for-byte golden file. It's enforced by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:22`, which calls `l.SetRole(RoleAdvisor)` then compares `l.Definitions()` against the file (one golden per role: `definitions.json` for no role, plus minion/copilot/advisor variants).

Regenerate it with:

```
go test ./internal/agent -run Golden -update
```

(On an accidental mismatch the test writes `definitions.advisor.json.got` next to the golden and tells you to diff; on purpose you use the `-update` flag above, `definitions_test.go:16-18`.)

## calls
search_code map[pattern:advisor]
semantic_search map[query:generated file that pins a list of tools for the advisor]
find_files map[pattern:definitions*]
search_code map[pattern:definitions\.(advisor|copilot)]
read_file map[path:internal/agent/definitions_test.go]

## tree after

