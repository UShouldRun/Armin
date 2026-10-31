Which file pins the advisor's tool list, and what command regenerates it?

right true, 35204 prompt (20032 cached), 438 completion, $0.00310, 4 rounds, 5 tools

## answer










The advisor's tool list is pinned in `internal/agent/testdata/definitions.advisor.json` — a golden file written by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:28-60`, which snapshots each role's tool definitions byte-for-byte so an accidental schema/description change (which would break prompt caching) fails the test.

To regenerate it:

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (`internal/agent/definitions_test.go:15-16`) rewrites all the `testdata/definitions*.json` files. There's also a machine assertion that the advisor is never offered `edit_file`, `write_file`, or `run_program` in `TestRoleToolMasks` (`internal/agent/role_test.go:19-23`), but the pinned list itself is the golden file.

## calls
search_code map[pattern:advisor]
find_files map[pattern:*advisor*]
search_code map[path:internal/agent pattern:definitions\.]
read_file map[path:internal/agent/role_test.go]
read_file map[path:internal/agent/definitions_test.go]

## tree after

