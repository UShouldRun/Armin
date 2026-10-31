Which file pins the advisor's tool list, and what command regenerates it?

right true, 27274 prompt (14272 cached), 441 completion, $0.00355, 3 rounds, 4 tools

## answer


The advisor's tool list is pinned in **`internal/agent/testdata/definitions.advisor.json`** — a golden file, one per role (`definitions.json`, `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json`).

It's kept byte-stable by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:21-52`, which marshals each role's `l.Definitions()` and compares it to the golden file (writing a `.got` file on mismatch for diffing).

To regenerate it deliberately:

```
go test ./internal/agent -run Golden -update
```

This is spelled out in the test's doc comment (`internal/agent/definitions_test.go:18-21`). The rationale: an accidental change to any tool description or schema changes model behavior and invalidates every prompt cache.

## calls
search_code map[pattern:advisor]
search_code map[pattern:pins? the .*tool]
read_file map[path:internal/agent/definitions_test.go]
find_files map[path:internal/agent pattern:definitions.advisor.json]

## tree after

