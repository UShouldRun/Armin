Which file pins the advisor's tool list, and what command regenerates it?

right true, 29923 prompt (21504 cached), 409 completion, $0.00464, 3 rounds, 3 tools

## answer
The advisor's tool list is pinned in the golden file `internal/agent/testdata/definitions.advisor.json`.

- **Test:** `TestDefinitionsGolden` in `internal/agent/definitions_test.go:24-51` — it builds a minimal `Loop`, calls `l.SetRole(RoleAdvisor)`, JSON-marshals `l.Definitions()`, and compares byte-for-byte against that file (`checkGolden`, definitions_test.go:66-80).
- **Regenerate on purpose:** `go test ./internal/agent -run Golden -update` — the `-update` flag (definitions_test.go:18-19) rewrites all the `testdata/definitions*.json` files.

The pin exists because an accidental change to a tool description or schema changes model behavior and throws away the prompt cache; on a failure the test also writes `definitions.advisor.json.got` for diffing (definitions_test.go:76-78).

## calls
search_code map[pattern:advisor]
search_code map[pattern:tool list]
read_file map[path:internal/agent/definitions_test.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793475801301765362"
author a2 armin session "bench-1793475801301765362"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/golden-pinning decision checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned byte-for-byte by the golden-file test TestDefinitionsGolden (internal/agent/definitions_test.go): it dumps l.Definitions() per role plus the bare ToolDefinitions() to testdata/definitions[.<role>].json for minion/copilot/advisor and fails on any diff, writing the new bytes to <golden>.got. Regenerate deliberately with `go test ./internal/agent -run Golden -update`. Per-role pinning exists because a changed tool schema changes model behavior and invalidates prompt caches."
  evidence file "internal/agent/definitions_test.go:22-80" 2eb3c6bd
  evidence file "internal/agent/tools.go:103-147" 156309bd

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go"
  evidence session "internal/agent/definitions_test.go" 00000000

