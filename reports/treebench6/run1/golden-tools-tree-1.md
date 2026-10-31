How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 41066 prompt (29792 cached), 689 completion, $0.00256, 4 rounds, 5 tools

## answer


It's a **golden-file test that pins each role's tool definitions byte for byte**.

**The test** — `TestDefinitionsGolden` in `internal/agent/definitions_test.go:26`:
- Marshals the full tool list (`l.Definitions()` + the sandbox-only `ToolDefinitions()`) to JSON and compares it against `internal/agent/testdata/definitions.json`.
- Then, per role — `RoleMinion`, `RoleCopilot`, `RoleAdvisor` — it calls `l.SetRole(r)` and pins each role's list in its own file: `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` (all in `internal/agent/testdata/`).

**The check** — `checkGolden` (`definitions_test.go:67`):
- If the marshaled output differs from the golden file, it writes `<file>.got` and fails with `tool definitions changed; diff testdata/...{,.got}`.
- A deliberate change is made with `go test ./internal/agent -run Golden -update`, which rewrites the golden files.

**Why byte-level per role**: the test's comment says it — any accidental change to a description or schema changes model behavior and throws away the prompt cache, so each role's list must stay byte-stable, not just semantically equivalent. This also pins the *order* (definitions are built in `ToolID` table order, `tools.go:141-145`) and the role permission mask, since reordering or an added/dropped tool changes the bytes.

**Runtime side** — `Loop.Definitions()` memoizes per role in `l.roleDefs` (`tools.go:143-155`), built once per role via `once.Do`, so the exact same definitions are reused for every call within a role.

## calls
search_code map[pattern:tool definitions]
search_code map[pattern:tools.*stable|stable.*tools]
read_file map[path:internal/agent/definitions_test.go]
search_code map[path:internal/agent/tools.go pattern:roleDefs|Definitions\(\)]
find_files map[path:internal/agent/testdata pattern:definitions*.json]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473424974534498"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-make file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:26 definitions_test.go:67 tools.go:141-145 tools.go:143-155"
  evidence session "internal/agent/definitions_test.go" 00000000

