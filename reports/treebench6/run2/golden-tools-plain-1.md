How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 55908 prompt (31360 cached), 960 completion, $0.00372, 6 rounds, 8 tools

## answer


It's guarded at two levels:

**1. Built once, immutable per role at runtime** (`internal/agent/tools.go:147-168`)
Each `Loop` holds `roleDefs [4]roleDefs`, one slot per `Role.slot()` (loop.type.go:101). The first time `Definitions()` is called in a given role, a `sync.Once` builds that role's list — filtered through `l.offered(id)`/`allows()` — and it is never rebuilt. So a role's tool set is frozen for the life of the Loop.

**2. A golden-file test pins the output byte-for-byte, per role** (`internal/agent/definitions_test.go`)
`TestDefinitionsGolden` marshals the definitions with `json.MarshalIndent` and compares against checked-in golden files in `testdata/`:
- `definitions.json` — the no-role list (plus the bare `ToolDefinitions()` table)
- `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` — one per role

Any accidental change to a description or schema fails the test, writing the new output to `testdata/<name>.got` with the hint "diff testdata/<name>{,.got}". The comment explains the stakes: "an accidental change to a description or a schema changes model behavior and throws away every prompt cache." A deliberate change is regenerated with `go test ./internal/agent -run Golden -update`.

The motivation is the prompt cache: since the tool list is sent with every request, armin keeps it byte-stable per role, and even role *switches* are handled by prefixing a reminder to the next user message instead of rewriting the system prompt (role.go:2-5), so everything before it stays cached.

## calls
search_code map[file_type:go pattern:role.*tool|tool.*role]
semantic_search map[query:tool definitions per role snapshot hash stable]
read_file map[path:internal/agent/role.type.go]
search_code map[file_type:go path:internal/agent pattern:definitions.json]
read_file map[path:internal/agent/role.go]
read_file map[path:internal/agent/definitions_test.go]
search_code map[file_type:go path:internal/agent pattern:roleDefs|func (l \*Loop) Definitions]
read_file map[end_line:175 path:internal/agent/tools.go start_line:140]

## tree after

