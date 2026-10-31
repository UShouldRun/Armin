Which file pins the advisor's tool list, and what command regenerates it?

right true, 42773 prompt (30784 cached), 493 completion, $0.00432, 5 rounds, 6 tools

## answer


The advisor's tool list is pinned in **`internal/agent/testdata/definitions.advisor.json`** — a golden file checked byte-for-byte by `TestDefinitionsGolden` (`internal/agent/definitions_test.go:26`), since any drift in a description or schema changes model behavior and invalidates the prompt cache.

To regenerate it (all roles):

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (`definitions_test.go:18-20`) rewrites the `testdata/definitions*.json` files from the current tool specs.

## calls
search_code map[pattern:advisor]
semantic_search map[query:advisor tool list generation regenerate]
find_files map[pattern:definitions.*.json]
search_code map[path:internal/agent pattern:-update]
search_code map[path:internal/agent pattern:update]
read_file map[end_line:40 path:internal/agent/definitions_test.go start_line:1]

## tree after

