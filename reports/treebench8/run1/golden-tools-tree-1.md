How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 91708 prompt (58080 cached), 1106 completion, $0.00950, 7 rounds, 8 tools

## answer
Armin pins the tool definitions with **golden-file tests**. The mechanism is in `internal/agent/definitions_test.go`:

- **`TestDefinitionsGolden`** (definitions_test.go:22) builds a `Loop`, serializes what the model would be told to JSON, and compares it byte-for-byte against golden files in `internal/agent/testdata/`:
  - `definitions.json` — all tools plus the bare `ToolDefinitions()` set (tools.go:103, what `mcpserver` serves)
  - one file **per role**: `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` — each produced by calling `l.SetRole(r)` and dumping `l.Definitions()` (tools.go:145), which filters by `id.allows(l.Role)` via `offered` (tools.go:116)

- **On mismatch**, `checkGolden` (definitions_test.go:63) writes the new output to `<golden>.got` and fails the test with `diff testdata/<name>{,.got}` — so you diff instead of guessing what changed.

- **Deliberate changes** are made on purpose with `go test ./internal/agent -run Golden -update`, which rewrites the testdata files (`-update` flag, definitions_test.go:15).

Why it's pinned at all, per the test's doc comment (definitions_test.go:19-21): an accidental change to a tool description or schema silently changes model behavior **and invalidates every prompt cache** — including when switching roles mid-session, which is why each role's list gets its own golden file.

(Recorded as [n3] in the knowledge tree.)

## calls
search_code map[file_type:go pattern:hash|fingerprint|digest]
search_code map[file_type:go pattern:role]
search_code map[file_type:go pattern:golden]
read_file map[path:internal/agent/definitions_test.go]
search_code map[file_type:go path:internal/agent pattern:ToolDefs|ToolsFor|func.*Tools]
search_code map[file_type:go path:internal/agent pattern:func \(l \*Loop\) Definitions|func \(l \*Loop\) SetRole|func ToolDefinitions|func \(l \*Loop\) …
run_command map[args:[internal/agent/testdata] binary:ls]
know_write map[claim:Tool definitions sent to the model are pinned byte-for-byte by the golden-file test TestDefinitionsGolden (internal/agent/definiti…

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

