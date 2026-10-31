How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 48667 prompt (31360 cached), 938 completion, $0.00338, 5 rounds, 7 tools

## answer


Armin pins the tool definitions with **golden-file tests, byte for byte, per role**.

**The test** — `TestDefinitionsGolden` in `internal/agent/definitions_test.go`:
- Builds a `Loop` with fixed test fixtures (a demo skill, a library), marshals `l.Definitions()` to indented JSON, and compares it byte-for-byte against `internal/agent/testdata/definitions.json` — one for the full (no-role) list.
- Then loops over each role — `RoleMinion`, `RoleCopilot`, `RoleAdvisor` — calls `l.SetRole(r)` and compares that role's list against `testdata/definitions.{minion,copilot,advisor.json}`. So each role's exact list gets its own golden file (`internal/agent/definitions_test.go:48-54`).
- On mismatch it writes `definitions.<name>.json.got` next to the golden so you can diff, and fails the build.
- Deliberate changes are made with `go test ./internal/agent -run Golden -update`, which rewrites the testdata files.

**Why it matters** (stated in the doc comment at `definitions_test.go:19-22`): an accidental change to a description or schema silently changes model behavior, and — because the definitions go at the front of every request — throws away the server's prompt cache. The per-role files exist for the same reason on role *switches*: "each role's list must stay byte-stable too, or switching roles throws the prompt cache away" (`definitions_test.go:47-48`).

**Supporting structure** that makes the test meaningful:
- The definitions are built **once per role** and then immutable: `Loop.roleDefs` is a `[4]roleDefs` array, one slot per role, and `defsFor()` guards construction with `sync.Once` (`internal/agent/loop.type.go:109`, `internal/agent/tools.go:149-174`). So within a session the same list object is sent every request, and the golden test locks down what first construction produces.
- A second, cheaper test `TestToolDefinitionsShape` (`internal/agent/loop_test.go`) sanity-checks the shape (e.g., exactly 38 tools), while the golden test checks the content.

## calls
search_code map[pattern:tool definitions]
semantic_search map[query:ensuring tool definitions sent to model are stable unchanged per role]
read_file map[path:internal/agent/definitions_test.go]
read_file map[path:internal/agent/role.type.go]
run_command map[args:[internal/agent/testdata] binary:ls]
search_code map[path:internal/agent pattern:roleDefs|SetRole]
read_file map[end_line:200 path:internal/agent/tools.go start_line:130]

## tree after

