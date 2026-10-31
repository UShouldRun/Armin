How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 33791 prompt (17248 cached), 612 completion, $0.00235, 4 rounds, 5 tools

## answer


It's a golden-file test: `TestDefinitionsGolden` in internal/agent/definitions_test.go:26.

- It builds a fixed `Loop` with a fixed demo skill and library, then serializes the tool definitions to JSON and compares them **byte for byte** against golden files in `internal/agent/testdata/`: one for the default list (`definitions.json`) plus one per role — `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` — after calling `l.SetRole(r)` for each (definitions_test.go:52-57).
- Why byte-for-byte per role: the comment says an accidental change to a description or schema changes model behavior and invalidates the prompt cache, and switching roles must likewise keep its list stable or role-switching throws the cache away.
- On mismatch it writes `testdata/<name>.got` and fails with a hint to `diff testdata/<name>{,.got}` (definitions_test.go:74-76).
- Deliberate changes are made by regenerating with `go test ./internal/agent -run Golden -update` (definitions_test.go:70-73), so any drift is either caught or explicitly re-blessed.

## calls
search_code map[pattern:role]
list_directory map[]
search_code map[path:internal/agent pattern:TestDefinitions]
list_directory map[path:internal/agent/testdata]
read_file map[path:internal/agent/definitions_test.go]

## tree after

