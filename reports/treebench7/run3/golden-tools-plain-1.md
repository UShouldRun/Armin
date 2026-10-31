How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 42357 prompt (26656 cached), 957 completion, $0.00327, 4 rounds, 6 tools

## answer


Armin pins the tool definitions with **golden files**, one per role, checked byte-for-byte in a test.

**The mechanism** — `internal/agent/definitions_test.go`:

1. `TestDefinitionsGolden` builds a fully-equipped `Loop` (skills, library, know tree, minion, learner all wired up) and serializes its tool definitions with `json.MarshalIndent`, then compares against a golden file:
   - `testdata/definitions.json` — the full loop's list plus the bare `ToolDefinitions()` (the sandboxed-only set served by the MCP server).
   - `testdata/definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` — one per role, built after `l.SetRole(r)`, since each role offers a different subset via `offered()` / `id.allows(l.Role)` in `internal/agent/tools.go:130`.

2. Any mismatch fails the test and writes the new output next to the golden as `.got` so you can `diff testdata/definitions.<role>.json{,.got}` — see `checkGolden` at `definitions_test.go:63`.

3. Changes are deliberate and made on purpose with:
   ```
   go test ./internal/agent -run Golden -update
   ```
   which rewrites the golden files (the `-update` flag at `definitions_test.go:17`).

**Why it matters** (per the test's own comment, `definitions_test.go:20`):
- An accidental change to a description or schema silently changes model behavior.
- The definitions go at the front of every request, so any change invalidates the prompt cache — the per-role files exist specifically because switching roles shouldn't churn the cache either.

Two supporting details:
- `Loop.Definitions()` is built **once per role and cached** via a `sync.Once` in `defsFor()` (`tools.go:138`) — so within a session the list is constructed a single time and can't drift between rounds.
- There's also a shape test, `TestToolDefinitionsShape` in `loop_test.go`, which asserts the exact count (38) of `ToolDefinitions()`, catching tools added or dropped from the sandboxed set.

## calls
semantic_search map[count:10 query:tool definitions per role]
search_code map[file_type:go pattern:tools]
read_file map[path:internal/agent/tools.go]
search_code map[file_type:go path:internal/agent pattern:definitions.json|golden|sha256|goldenfile]
read_file map[path:internal/agent/definitions_test.go]
run_command map[args:[internal/agent/testdata] binary:ls]

## tree after

