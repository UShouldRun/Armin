How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 33143 prompt (26624 cached), 823 completion, $0.00576, 4 rounds, 5 tools

## answer
Armin pins the per-role tool definitions with **golden-file tests** — the exact JSON that goes to the model is checked in byte-for-byte, so any accidental change fails the test suite.

**The test** — `TestDefinitionsGolden` in `internal/agent/definitions_test.go:24`:

1. Builds a representative `Loop` (demo skill, empty library, notes, scratchpad) and marshals `l.Definitions()` to indented JSON, plus the bare `ToolDefinitions()` schema used by the MCP server.
2. Byte-compares against `internal/agent/testdata/definitions.json` (the no-role list) via `checkGolden` (definitions_test.go:70).
3. Then loops over the three roles — `l.SetRole(r)` for `RoleMinion`, `RoleCopilot`, `RoleAdvisor` — and does the same byte-exact check against its own golden file: `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json`. All four files exist under `internal/agent/testdata/`.
4. On mismatch it writes `definitions.<role>.json.got` next to the golden and fails with `diff testdata/<file>{,.got}` (definitions_test.go:78-80).

**Why it matters** (comment at definitions_test.go:19-22): an accidental change to a description or schema changes model behavior and throws away every prompt cache; switching roles also has to produce a byte-stable list, or role switches bust the cache.

**Intentional changes** are deliberate: regenerate with
`go test ./internal/agent -run Golden -update` (the `-update` flag at definitions_test.go:15, handled in `checkGolden`).

**What makes the golden files meaningful** — each `toolSpec` carries a bitmask of allowed roles, and `Loop.Definitions()` (tools.go:145) returns the role-filtered list built once per role (`defsFor` → `roleDefs[l.Role.slot()]`, tools.go:149; filtering via `offered`/`ToolID.allows` at tools.go:116 and role.go:83). So the snapshot captures exactly what those filters produce per role; touching a spec's `roles`, description, or parameter schema in any tool file will trip the test.

## calls
semantic_search map[query:tool definitions per role stable hash check]
search_code map[pattern:tool.*definition|ToolDef|tools.*role]
read_file map[path:internal/agent/definitions_test.go]
find_files map[path:internal/agent/testdata pattern:definitions*.json]
search_code map[path:internal/agent pattern:func \(l \*Loop\) Definitions|func \(l \*Loop\) offered|roleDefs|func.*allows]

## tree after

