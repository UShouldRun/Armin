Which file pins the advisor's tool list, and what command regenerates it?

right true, 45653 prompt (30272 cached), 582 completion, $0.00405, 4 rounds, 6 tools

## answer








The reports are past run outputs — let me verify against the actual source.




**File:** `internal/agent/testdata/definitions.advisor.json` — a golden file, checked byte-for-byte by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:26`. It's one of four (there are also `definitions.json`, `.minion.json`, `.copilot.json`). The pin exists because any accidental change to a tool description or schema changes model behavior and invalidates prompt caches.

**Regenerate:**

```
go test ./internal/agent -run Golden -update
```

The `-update` flag is declared in `definitions_test.go:18-20`; when set, `checkGolden` rewrites the golden files (definitions_test.go:66-69). Without it, a mismatch writes the new output to `definitions.advisor.json.got` and fails with a hint to diff.

## calls
search_code map[pattern:advisor]
search_code map[pattern:tool list]
search_code map[pattern:definitions.advisor.json]
search_code map[file_type:go pattern:-update]
search_code map[path:internal/agent pattern:update]
find_files map[path:internal/agent pattern:definitions.advisor.json]

## tree after

