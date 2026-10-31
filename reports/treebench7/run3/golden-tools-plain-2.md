Which file pins the advisor's tool list, and what command regenerates it?

right true, 22961 prompt (17472 cached), 399 completion, $0.00249, 3 rounds, 4 tools

## answer


The advisor's tool list is pinned in the golden file **`internal/agent/testdata/definitions.advisor.json`**.

It's enforced by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:22`, which marshals `Loop.Definitions()` for each role (minion, copilot, advisor) and requires a byte-for-byte match — the tool list is sent with every request, so any change silently kills the prompt cache.

To regenerate it deliberately:

```
go test ./internal/agent -run Golden -update
```

This rewrites the `testdata/definitions*.json` files (the `-update` flag is defined at `internal/agent/definitions_test.go:17`). Without `-update`, a mismatch writes out `definitions.advisor.json.got` for you to diff.

## calls
semantic_search map[query:advisor tool list pinned generated regenerate]
search_code map[pattern:regenerat]
read_file map[path:internal/agent/definitions_test.go]
find_files map[pattern:definitions.advisor.json]

## tree after

