How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 71537 prompt (58624 cached), 1366 completion, $0.01127, 6 rounds, 9 tools

## answer
Let me look at the actual code, not just the report files from previous runs.

Now let me find the actual golden-file test that enforces this.

Let me read the full test to see the per-role part.

Armin pins the tool definitions with **golden-file tests** — the exact JSON sent to the model is checked in byte-for-byte, so any accidental change to a description or schema fails the test suite.

**The test** — `TestDefinitionsGolden` in `internal/agent/definitions_test.go:26`:
- Builds a fully-loaded `Loop` (memories, scratch, skills, library, know, minion, learner) so `Definitions()` reflects a real offering.
- Marshals the definitions to indented JSON and compares against `testdata/definitions.json` (the full/`ToolDefinitions()` list).
- Then loops over each role — `RoleMinion`, `RoleCopilot`, `RoleAdvisor` (`definitions_test.go:55-60`) — calls `l.SetRole(r)` and checks each against its own golden file: `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json`.
- Comparison is a strict `string(got) != string(want)` byte-equality (`checkGolden`, `definitions_test.go:76`). On mismatch it writes a `.got` side and fails with "tool definitions changed; diff testdata/…".

**Per-role stability** is the point: each role gets its own golden file because the role's allowed-tool set (via the `offered`/`allows` bitmask, `tools.go:124`) determines which definitions are sent. If any tool's description, parameter, or the role's tool set drifts, that role's golden test fails.

**Deliberate changes** are opt-in via the `-update` flag, which rewrites the goldens: `go test ./internal/agent -run Golden -update` (`definitions_test.go:66-68`).

**Why byte-stable:** the comment at `definitions_test.go:23-24` notes that changing a definition changes model behavior *and* throws away every prompt cache — so both the model-visible text and the cached prefix must stay stable. This matches the caching design in `docs/design/restructure-plan.md:152` ("byte-stable per role, one golden file per role").

The 4 golden files live in `internal/agent/testdata/`: `definitions.json` plus one per role.

## calls
search_code map[pattern:tool definitions]
semantic_search map[query:stable tool definitions per role hash check prevent accidental changes]
read_file map[path:internal/agent/tools.go]
search_code map[pathinternal:agent/definitions_test.go pattern:golden|definitions\*.json|TestDefinitions]
read_file map[end_line:110 path:docs/design/handoff-claude.md start_line:90]
find_files map[pathinternal:agent pattern:definitions*]
search_code map[path:internal/agent pattern:TestDefinitions|golden|definitions\.json|roleDefs]
read_file map[path:internal/agent/definitions_test.go]
find_files map[path:internal/agent/testdata pattern:definitions*.json]

## tree after

