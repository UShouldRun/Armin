How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right false, 59924 prompt (32928 cached), 1339 completion, $0.00468, 5 rounds, 8 tools

## answer


The old benchmark reports point at the right files; let me verify against the actual source.





Armin pins the tool definitions with **golden-file tests**, byte for byte, per role — plus deterministic construction at runtime.

**The golden test** — `TestDefinitionsGolden` (`internal/agent/definitions_test.go:24`):
- Builds a representative `Loop` (with a demo skill, an empty library, notes, scratchpad) so the tool list is complete and reproducible.
- Marshals `l.Definitions()` to indented JSON and compares it against the checked-in `internal/agent/testdata/definitions.json` (which also includes the bare `ToolDefinitions()` list the MCP server serves).
- Then loops over the three roles — `l.SetRole(r)` for `minion`, `copilot`, `advisor` — and does the same byte-exact check against its own golden file: `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json`. All four are in `internal/agent/testdata/`.
- On mismatch it writes `definitions.<role>.json.got` and fails with a hint to `diff testdata/...{,.got}`. Intentional changes are regenerated with `go test ./internal/agent -run Golden -update`.

The comment in the test states the why: an accidental change to a description or schema changes model behavior and **throws away the prompt cache** — so per-role byte-stability is the invariant, not just semantic equivalence (the golden files also pin each role's permission mask, since which tools a role sees comes from `offered()` in `tools.go`).

**Runtime determinism** — the definitions can't drift even without the test:
- Tool order is a fixed table in `internal/agent/toolid.go:2-7`; the comment says it plainly: "Definitions walks them in order, so moving one changes the prompt."
- `Loop.Definitions()` (`internal/agent/tools.go:145`) returns a list built **once per role** via `sync.Once` (`defsFor`, tools.go:149-170), so the exact same slice goes out with every request in that role — no re-marshaling, no reordering between calls.
- A role switch doesn't rebuild anything: `SetRole` just queues a reminder for the next user message, leaving the tool block cache-valid.

There's also a lighter shape test, `TestToolDefinitionsShape` in `internal/agent/loop_test.go`, checking entry count, `type: "function"`, non-empty descriptions, and an explicit expected name list.



(Also recorded as n3 in the knowledge tree — it updated the existing `agent/tools/definition-stability` node rather than duplicating it.)

## calls
search_code map[pattern:tool definitions]
search_code map[pattern:tool_definitions|toolDefinitions|ToolDefinition]
read_file map[path:internal/agent/definitions_test.go]
search_code map[path:internal/agent pattern:func \(l \*Loop\) Definitions|func ToolDefinitions|roleDefs|sync.Once]
find_files map[path:internal/agent/testdata pattern:definitions*.json]
read_file map[end_line:20 path:internal/agent/toolid.go start_line:1]
read_file map[end_line:175 path:internal/agent/tools.go start_line:143]
know_write map[claim:Tool definitions sent to the model are pinned per role: TestDefinitionsGolden (internal/agent/definitions_test.go) byte-compares m…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474423248149932"
author a2 armin session "bench-1793474423248149932"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/definition-stability fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned per role: TestDefinitionsGolden (internal/agent/definitions_test.go) byte-compares marshaled Loop.Definitions() against testdata/definitions.json plus one golden file per role (minion/copilot/advisor); regen with -update. Why: a change breaks the prompt cache and model behavior. At runtime the list is built once per role via sync.Once in defsFor (tools.go), walking the fixed table order in toolid.go, so every request in a role sends the identical list. A shape test TestToolDefinitionsShape (loop_test.go) checks count, type, descriptions, names."
  evidence file "internal/agent/definitions_test.go:24-80" 04c0f17e
  evidence file "internal/agent/tools.go:143-170" 9b5983df
  evidence file "internal/agent/toolid.go:2-7" b97197a5

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go internal/agent/toolid.go:1-20 internal/agent/tools.go:143-175"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence file "internal/agent/toolid.go:1-20" 9fa27f69
  evidence file "internal/agent/tools.go:143-175" a7eb1832

