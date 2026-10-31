Which file pins the advisor's tool list, and what command regenerates it?

right true, 59319 prompt (51200 cached), 1098 completion, $0.00898, 6 rounds, 8 tools

## answer
Confirmed from the test itself:

- **Pinned file:** `internal/agent/testdata/definitions.advisor.json` — one golden file per role (`definitions.minion.json`, `definitions.copilot.json`, plus the all-tools `definitions.json`). The test `TestDefinitionsGolden` in `internal/agent/definitions_test.go:22` compares each role's `Loop.Definitions()` byte-for-byte against it, so any accidental drift in a tool name, description, or schema fails the test (it would also bust the prompt cache, which the test comment at line 18–20 explains).
- **Regeneration:** `go test ./internal/agent -run Golden -update` (stated in the doc comment at `internal/agent/definitions_test.go:19`; the `-update` flag at line 16 rewrites the `testdata/definitions*.json` files).

Both facts are now recorded in the knowledge tree — [n2] pins the files, [n6] the regeneration command.

**Short answer:** the advisor's tool list is pinned in `internal/agent/testdata/definitions.advisor.json`, and you regenerate it with `go test ./internal/agent -run Golden -update`.

## calls
search_code map[pattern:advisor]
list_languages map[]
find_files map[pattern:definitions.*json]
search_code map[file_type:go pattern:definitions\.]
read_file map[path:internal/agent/definitions_test.go]
know_write map[claim:Each role's tool list is pinned byte-for-byte in testdata/definitions.<role>.json (advisor, minion, copilot, plus all-tools defini…
know_write map[claim:Regenerate the golden tool-definition files with: go test ./internal/agent -run Golden -update confidence:checked derived_from:[ag…
know_write map[claim:Regenerate the golden tool-definition files with: go test ./internal/agent -run Golden -update confidence:checked derived_from:[n2…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474423248149932"
author a2 armin session "bench-1793474423248149932"
author a3 copilot model "bench" session "bench-1793474448580302757"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each role's tool list is pinned byte-for-byte in testdata/definitions.<role>.json (advisor, minion, copilot, plus all-tools definitions.json) by TestDefinitionsGolden, so a drift in any tool name, description or schema fails the test and would throw away the prompt cache."

n3 agent/tools/definition-stability fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned per role: TestDefinitionsGolden (internal/agent/definitions_test.go) byte-compares marshaled Loop.Definitions() against testdata/definitions.json plus one golden file per role (minion/copilot/advisor); regen with -update. Why: a change breaks the prompt cache and model behavior. At runtime the list is built once per role via sync.Once in defsFor (tools.go), walking the fixed table order in toolid.go, so every request in a role sends the identical list. A shape test TestToolDefinitionsShape (loop_test.go) checks count, type, descriptions, names."
  evidence file "internal/agent/definitions_test.go:24-80" 04c0f17e
  evidence file "internal/agent/tools.go:143-170" 9b5983df
  evidence file "internal/agent/toolid.go:2-7" b97197a5

n6 agent/tools/regeneration-command decision checked a3 born 2026-10-31 seen 2026-10-31
  claim "Regenerate the golden tool-definition files with: go test ./internal/agent -run Golden -update"
  derived-from n2
  evidence file "internal/agent/definitions_test.go:19" 9dec84fe

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go internal/agent/toolid.go:1-20 internal/agent/tools.go:143-175"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence file "internal/agent/toolid.go:1-20" 9fa27f69
  evidence file "internal/agent/tools.go:143-175" a7eb1832

