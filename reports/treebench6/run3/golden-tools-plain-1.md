How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 59237 prompt (40768 cached), 952 completion, $0.00365, 6 rounds, 8 tools

## answer


Armin pins them with golden-file tests, one per role.

**The test** — `internal/agent/definitions_test.go:26` (`TestDefinitionsGolden`):
- Builds a fully-featured `Loop` (memories, scratchpad, skills, library, know graph, learner all present) and marshals `l.Definitions()` plus the bare `ToolDefinitions()` (the sandboxed set mcpserver serves).
- The result must match `testdata/definitions.json` **byte for byte**.
- Then, for each role, it calls `l.SetRole(r)` for `RoleMinion`, `RoleCopilot`, `RoleAdvisor` and compares that role's list against its own golden: `testdata/definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json`. On mismatch it writes `<file>.got` so you can `diff testdata/definitions.X.json{,.got}`.

**Why per role**: each role gets a different subset (the `offered` check in `internal/agent/tools.go:115` filters by role via `id.allows(l.Role)` plus which stores the Loop has), and the lists are cached once per role in `defsFor()` (`tools.go:161`). Any accidental drift in a description or schema would change what the model is told *and* throw away the prompt cache, so every role's wire format is pinned independently.

**Changing on purpose**: `go test ./internal/agent -run Golden -update` rewrites the testdata files.

There's also a shape test, `TestToolDefinitionsShape` in `internal/agent/loop_test.go:19`, which asserts the bare list has exactly 38 tools and includes a named subset — a coarser guard against accidental tool drops/adds.

## calls
semantic_search map[query:tool definitions per role sent to model, prevent accidental change]
search_code map[file_type:go pattern:role]
search_code map[file_type:go path:internal/agent pattern:golden|definitions\.json]
search_code map[file_type:go path:internal/agent pattern:ToolDefinitions]
read_file map[path:internal/agent/definitions_test.go]
run_command map[args:[internal/agent/testdata] binary:ls]
search_code map[file_type:go path:internal/agent pattern:func \(l \*Loop\) (Definitions|SetRole)]
read_file map[end_line:180 path:internal/agent/tools.go start_line:100]

## tree after

