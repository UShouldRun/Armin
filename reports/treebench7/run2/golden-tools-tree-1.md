How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 35817 prompt (25088 cached), 753 completion, $0.00259, 4 rounds, 5 tools

## answer


With **golden-file tests**: the exact JSON tool definitions are pinned, byte for byte, in a checked-in file per role, and any accidental change fails the test suite.

**The mechanism** — `TestDefinitionsGolden` in `internal/agent/definitions_test.go:26`:

1. It builds a `Loop` with deterministic fixtures (a temp skills dir with one demo skill, an empty library, fresh know tree) so the definitions are reproducible.
2. It compares the output of `l.Definitions()` against `internal/agent/testdata/definitions.json` (the no-role list; it also pins the bare `ToolDefinitions()` table in the same file).
3. Then it loops over `RoleMinion`, `RoleCopilot`, `RoleAdvisor`, calling `l.SetRole(r)` and pinning each role's list in its own file: `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` (all present in `internal/agent/testdata/`). The comment at definitions_test.go:44 says why: *"each role's list must stay byte-stable too, or switching roles throws the prompt cache away."*
4. `checkGolden` (definitions_test.go:61) does a strict `string(got) != string(want)` compare. On a mismatch it writes `definitions.<role>.json.got` next to the golden so you can `diff`, and fails with "tool definitions changed".

**Updating deliberately** is opt-in, not a default pass: `go test ./internal/agent -run Golden -update` rewrites the testdata files (the `-update` flag at definitions_test.go:19).

There's also a coarser shape check, `TestToolDefinitionsShape` in `internal/agent/loop_test.go`, which asserts the entry count and per-tool schema shape — a fast tripwire on top of the byte-exact golden files.

The stated rationale (definitions_test.go:22-25): any change to a description or schema changes model behavior and invalidates every prompt cache, so even "harmless" edits have to be explicit.

## calls
search_code map[pattern:tool definitions]
semantic_search map[query:stable tool definitions per role snapshot]
search_code map[path:internal/agent pattern:definitions.*-update|TestDefinitionsGolden]
run_command map[args:[internal/agent/testdata] binary:ls]
read_file map[path:internal/agent/definitions_test.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474471069409311"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-make file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:26 definitions_test.go:44 definitions_test.go:61 definitions_test.go:19 definitions_test.go:22-25"
  evidence session "internal/agent/definitions_test.go" 00000000

