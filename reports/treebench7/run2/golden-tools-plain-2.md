Which file pins the advisor's tool list, and what command regenerates it?

right true, 51785 prompt (34432 cached), 630 completion, $0.00558, 5 rounds, 7 tools

## answer


**Pinned by:** `internal/agent/testdata/definitions.advisor.json` — the golden file checked by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:24`. The test marshals `Loop.Definitions()` with the advisor role set and compares byte-for-byte, so any change to a tool description or schema (which would also invalidate prompt caches) fails the test and writes `definitions.advisor.json.got` for you to diff.

**Regenerate it with:**

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (declared at `definitions_test.go:18`) rewrites all the golden files — `definitions.json` plus one per role (`definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json`) — so it regenerates the whole set, not just the advisor's.

## calls
semantic_search map[query:advisor tool list pinned generated]
search_code map[pattern:advisor]
search_code map[file_type:go pattern:go:generate]
search_code map[file_type:go pattern:advisor|Advisor]
search_code map[pattern:definitions\.advisor\.json]
find_files map[pattern:*advisor*.json]
read_file map[path:internal/agent/definitions_test.go]

## tree after

