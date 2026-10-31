How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 65057 prompt (40768 cached), 929 completion, $0.00383, 6 rounds, 8 tools

## answer


It pins them with **golden-file tests** — the exact JSON the model sees, checked byte-for-byte, per role.

**Where:** `internal/agent/definitions_test.go`, `TestDefinitionsGolden` (definitions_test.go:26-66)

**How it works:**
1. Builds a `Loop` with a fixed temp environment (a demo skill, an empty library) so the output is deterministic.
2. Marshals the full tool list (`l.Definitions()`) plus the sandbox-only list (`ToolDefinitions()`) to JSON and compares against `testdata/definitions.json`.
3. Then loops over each role — `RoleMinion`, `RoleCopilot`, `RoleAdvisor` — calls `l.SetRole(r)`, marshals `l.Definitions()` again, and compares against a separate file: `testdata/definitions.{minion,copilot,advisor}.json` (definitions_test.go:60-66).
4. `checkGolden` (definitions_test.go:68-80): without the flag, any drift fails the test and writes a `.got` file so you can `diff testdata/definitions.X.json{,.got}`. With `-update` it rewrites the goldens.

**Why per-role:** the comment at definitions_test.go:20-22 and :58-59 states the rationale — an accidental change to a description or schema changes model behavior *and* throws away the prompt cache; and since each role offers a different tool list (e.g. minion has everything but `run_program`, advisor no file changes), switching roles must stay byte-stable or every role switch busts the cache.

**To change on purpose:** `go test ./internal/agent -run Golden -update`

One related detail: `Loop.Definitions()` (tools.go:143) returns a list built once per role and cached, so the same bytes are sent on every turn of that role — the golden files are what guarantee those bytes don't silently move.

## calls
search_code map[pattern:tool.?(definition|schema)]
search_code map[pattern:role]
read_file map[path:internal/agent/definitions_test.go]
find_files map[pattern:definitions*.json]
search_code map[path:internal/agent pattern:func (l \*Loop) Definitions|func (l \*Loop) SetRole]
find_definition map[from_file:internal/agent/definitions_test.go name:Definitions]
find_definition map[name:SetRole]
search_code map[path:internal/agent pattern:func.*Definitions|func.*SetRole]

## tree after

