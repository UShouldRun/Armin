Which file pins the advisor's tool list, and what command regenerates it?

right true, 36042 prompt (29440 cached), 468 completion, $0.00340, 4 rounds, 5 tools

## answer


The advisor's tool list is pinned byte-for-byte in the golden file **`internal/agent/testdata/definitions.advisor.json`**, checked by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:24-51`.

To regenerate it (deliberate change):

```
go test ./internal/agent -run Golden -update
```

The `-update` flag rewrites all the golden files (`definitions.json` plus one per role); without it, any drift fails the test and drops a `definitions.advisor.json.got` file for diffing. The rationale (definitions_test.go:21-23): an accidental description/schema change alters model behavior and invalidates the prompt cache.

## calls
search_code map[pattern:advisor]
semantic_search map[query:generated list of tools pinned for advisor]
search_code map[path:internal/agent pattern:definitions.advisor.json]
search_code map[pattern:update.*golden|golden.*update|UpdateGolden|write.*golden]
read_file map[path:internal/agent/definitions_test.go]

## tree after

