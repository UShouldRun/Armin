Which file pins the advisor's tool list, and what command regenerates it?

right true, 39090 prompt (31872 cached), 493 completion, $0.00345, 4 rounds, 5 tools

## answer










The advisor's tool list is pinned by the golden file `internal/agent/testdata/definitions.advisor.json`, checked byte-for-byte in `TestDefinitionsGolden` (`internal/agent/definitions_test.go:27`).

Regenerate it with:

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (handled in `checkGolden`, `definitions_test.go:68`) rewrites the goldens instead of comparing them — the same command also regenerates the no-role `definitions.json` and the minion/copilot files.

Docs confirm the reasoning: a drift in a description or schema changes model behavior and invalidates prompt caches (`docs/components/agent.md:50-52`).

## calls
search_code map[pattern:advisor]
semantic_search map[query:advisor tool list generation]
search_code map[pattern:definitions\.]
read_file map[end_line:110 path:internal/think/treebench_test.go start_line:40]
read_file map[end_line:70 path:internal/agent/definitions_test.go start_line:20]

## tree after

