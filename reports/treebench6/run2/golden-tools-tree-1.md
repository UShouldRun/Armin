How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 86236 prompt (65856 cached), 1717 completion, $0.00591, 8 rounds, 11 tools

## answer


[armin] Short version (as I answered above): a **golden-file test pins each role's tool list byte-for-byte**.

- `TestDefinitionsGolden` (internal/agent/definitions_test.go:24) marshals `Loop.Definitions()` for the all-tools case plus each of `RoleMinion`, `RoleCopilot`, `RoleAdvisor` and compares against `testdata/definitions.json` and `testdata/definitions.{minion,copilot,advisor}.json` with an exact string compare (definitions_test.go:62).
- The output is deterministic because `defsFor()` (internal/agent/tools.go:147) builds each role's list once (`sync.Once`), iterating the static spec table in fixed toolID order and filtering by role.
- Any accidental wording or schema change fails the test; it writes `testdata/<name>.got` for a diff. Deliberate changes are regenerated with `go test ./internal/agent -run Golden -update`.
- Reason (definitions_test.go:18-20): the definitions ride on every request, so an unintended change would both alter model behavior and invalidate the server's prompt cache — the per-role files keep each role's cache prefix stable across role switches.

## calls
know_find map[query:tool definitions stability per role]
list_directory map[depth:2]
search_code map[path:internal/agent pattern:tool.*defin]
search_code map[path:internal/tools pattern:role]
read_file map[path:internal/agent/definitions_test.go]
read_file map[path:internal/agent/role.type.go]
search_code map[path:internal/agent pattern:defsFor|SetRole|roleDefs]
read_file map[end_line:175 path:internal/agent/tools.go start_line:135]
read_file map[end_line:40 path:internal/agent/role.go start_line:1]
find_files map[path:internal/agent pattern:definitions*]
know_write map[claim:Tool definitions sent to the model are pinned byte-for-byte by the golden test TestDefinitionsGolden (internal/agent/definitions_t…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106339253"
author a2 armin session "bench-1793473478106339253"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/definitions-stability fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned byte-for-byte by the golden test TestDefinitionsGolden (internal/agent/definitions_test.go), with one golden file in testdata/ per role (definitions.json plus definitions.minion/copilot/advisor.json), because an accidental description/schema change would alter model behavior and invalidate the server prompt cache. They are deterministic: defsFor() (internal/agent/tools.go:147) builds each role's list once via sync.Once, in fixed toolID order, from the static spec table. Regenerate deliberately with: go test ./internal/agent -run Golden -update."
  evidence file "internal/agent/definitions_test.go:24-80" 04c0f17e
  evidence file "internal/agent/tools.go:142-163" 6e6e631f
  evidence file "internal/agent/role.type.go:23-31" 3c17162e

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:24 definitions_test.go:62 internal/agent/tools.go:147 definitions_test.go:18-20"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence file "internal/agent/role.go:1-40" 75f9a198
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:135-175" 83eb99cb

