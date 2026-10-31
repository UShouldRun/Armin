How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right false, 66228 prompt (39200 cached), 1140 completion, $0.00437, 6 rounds, 8 tools

## answer


It's a **golden-file test** that pins the exact JSON the model receives, byte for byte — and it does so per role.

- `TestDefinitionsGolden` in `internal/agent/definitions_test.go:22` serializes the tool definitions the model is told about (`l.Definitions()` in each role) and compares against committed golden files in `internal/agent/testdata/`:
  - `definitions.json` — the full unfiltered set (bare `ToolDefinitions()`, 38 tools)
  - `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` — one per role, produced by `l.SetRole(r)` then dumping `l.Definitions()` (definitions_test.go:54-60)
- `checkGolden` (definitions_test.go:62) is a plain string compare: any byte change to a description or schema fails the test with `tool definitions changed; diff testdata/….json{,.got}` — it even writes a `.got` file so you can diff what changed.
- The rationale (comment at definitions_test.go:20-21, echoed in docs/components/agent.md:50-52): an accidental wording change alters model behavior and invalidates the provider's prompt cache. Role lists matter specifically because switching roles re-sends the tool block, so a drift there throws the cache away.
- When a change *is* intentional, you regenerate with `go test ./internal/agent -run Golden -update` and commit the new files.

Ordering is also fixed by `toolid.go` (table order), so the pin covers order as well as content.



Fact saved to the knowledge tree [n3].

## calls
semantic_search map[query:tool definitions per role stable hash prevent accidental change]
search_code map[pattern:tools.*hash|hash.*tool|golden|snapshot.*tool]
search_code map[path:internal/agent pattern:TestToolDefinitionsShape|definitions\.json|ToolDefinitionsFor|roleTools|ToolsFor]
outline_file map[path:internal/agent/toolid.go]
read_file map[path:internal/agent/definitions_test.go]
run_command map[args:[internal/agent/testdata] binary:ls]
search_code map[path:internal/agent pattern:func \(l \*Loop\) Definitions|SetRole|roleMask|allowedTools]
know_write map[claim:Tool definitions sent to the model are pinned byte-for-byte by the golden test TestDefinitionsGolden (internal/agent/definitions_t…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474539444737731"
author a2 armin session "bench-1793474539444737731"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/golden-definitions fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned byte-for-byte by the golden test TestDefinitionsGolden (internal/agent/definitions_test.go): one golden file per role (definitions.{minion,copilot,advisor}.json + all-tools definitions.json) in internal/agent/testdata/. Any byte change fails the test and writes a .got file for diffing. Regenerate intentionally with go test ./internal/agent -run Golden -update. Reason: wording changes alter model behavior and invalidate the prompt cache; role lists are pinned because switching roles re-sends the tool block."
  evidence file "internal/agent/definitions_test.go:22-80" 2eb3c6bd
  evidence session "internal/agent/testdata/" 00000000

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go"
  evidence session "internal/agent/definitions_test.go" 00000000

