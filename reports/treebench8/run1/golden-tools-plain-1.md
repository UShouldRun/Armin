How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 32715 prompt (25600 cached), 859 completion, $0.00587, 4 rounds, 6 tools

## answer
Armin pins the tool definitions with a **golden-file test, one per role** — plus design choices that make the output deterministic so the pinning is meaningful:

1. **Single source of truth** — every tool's JSON schema lives once in the static `specs` table, in a fixed `ToolID` order (`internal/agent/tools.go`). `Loop.Definitions()` just filters that table by what the Loop offers and the role's permissions (`offered()`), so there's no per-call variation or reordering.

2. **Built once per role** — `defsFor()` (tools.go:142) caches each role's list behind a `sync.Once`, so even within a run the list can't drift.

3. **Byte-for-byte golden test** — `TestDefinitionsGolden` in `internal/agent/definitions_test.go:24` builds a `Loop` with fixed fixtures, marshals `l.Definitions()` with `json.MarshalIndent`, and compares against checked-in goldens:
   - `testdata/definitions.json` — full no-role list, plus the bare sandbox-only `ToolDefinitions()` table
   - `testdata/definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` — one exact list per role (the test loops `RoleMinion/Copilot/Advisor`, calls `l.SetRole(r)` each time)

4. **Drift fails the build** — any accidental wording/schema/order change makes the test fail and writes `definitions.<name>.json.got` next to the golden so you can `diff testdata/definitions.X.json{,.got}`. Deliberate changes are regenerated with `go test ./internal/agent -run Golden -update` (the `-update` flag in definitions_test.go:17).

The stated reason (definitions_test.go:19-21 comment): an accidental change alters model behavior and invalidates the provider's prompt cache. There's also `TestToolDefinitionsShape` (loop_test.go:19) as a coarser second line — it pins the 38-entry count and the presence of every tool name.

## calls
semantic_search map[query:tool definitions stable per role snapshot hash]
search_code map[file_type-:go pattern:tool definition]
search_code map[file_type:go pattern:ToolDefinitions]
search_code map[pattern:definitions.json]
read_file map[path:internal/agent/definitions_test.go]
read_file map[end_line:180 path:internal/agent/tools.go start_line:95]

## tree after

