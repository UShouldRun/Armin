How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 85830 prompt (75264 cached), 2275 completion, $0.01457, 8 rounds, 9 tools

## answer
Two layers, one runtime, one in CI:

**1. Built once per role and cached** — `defsFor()` (internal/agent/tools.go:150-177) wraps a `sync.Once` in a per-role slot (`l.roleDefs[l.Role.slot()]`). The list is walked exactly once per role: every `ToolID` is checked against `offered()` (role bitmask, read-only flag, flagMemories/scratch/skills/etc.), in fixed table order, then memoized. `Definitions()` just returns that slice. So within a session the bytes handed to the model are the same every request — important because the tool list goes on every request and any drift kills the prompt cache.

**2. A golden-file test pins those bytes, one file per role** — `TestDefinitionsGolden` (internal/agent/definitions_test.go:26-58) marshals `l.Definitions()` and bare `ToolDefinitions()` to JSON and byte-compares against `testdata/definitions.json` plus `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` (all four exist in internal/agent/testdata/). Any accidental change to a description or schema fails the test with a `.got` file to diff. It's updated deliberately with:

```
go test ./internal/agent -run Golden -update
```

The test comment spells out the motivation: an accidental description/schema change changes model behavior and throws away every prompt cache; even role switches are handled by prefixing a reminder to the next user message (internal/agent/role.type.go) rather than rewriting the system prompt, so the pinned bytes stay valid across switches too.

Two layers:

**1. Built once per role, cached** — `defsFor()` (internal/agent/tools.go:150-177) memoizes each role's list behind a `sync.Once` in `l.roleDefs[l.Role.slot()]`. The walk happens exactly once per role, in fixed table order: every `ToolID` is filtered through `offered()` (role bitmask, read-only flag, and capability flags), then the result is reused for every request in that role. So within a session the exact bytes sent to the model never change — that matters because the tool list rides on every request, and any drift kills the prompt cache.

**2. A golden-file test pins those bytes, one file per role** — `TestDefinitionsGolden` (internal/agent/definitions_test.go:26-58) marshals the definitions and byte-compares them against `testdata/definitions.json` plus `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` (all four exist). Any accidental change to a description or schema fails the test and drops a `.got` file to diff. It's regenerated deliberately with:

```
go test ./internal/agent -run Golden -update
```

The test comment spells out the motivation: an accidental change to a description or a schema changes model behavior and throws away every prompt cache. Related design: role *switches* don't rewrite the system prompt either — a reminder is prefixed to the next user message (internal/agent/role.type.go), so the pinned bytes stay valid across switches.

Two layers, one in the runtime, one in the tests:

**1. Built once per role and cached** — `defsFor()` (internal/agent/tools.go:150-177) stores a `sync.Once` in a per-role slot (`l.roleDefs[l.Role.slot()]`). On the first call for a role it walks the tool table in fixed order, filters each `ToolID` through `offered()` (role bitmask, read-only flag, capability flags), and memoizes the result. `Definitions()` just returns that cached slice, so within a session the bytes handed to the model never change — which matters because the tool list is sent with every request, and any drift would kill the prompt cache.

**2. A golden-file test pins those bytes, one file per role** — `TestDefinitionsGolden` (internal/agent/definitions_test.go:26-58) marshals the definitions to JSON and byte-compares against `testdata/definitions.json` plus `definitions.minion.json`, `definitions.copilot.json`, `definitions.advisor.json` (all four exist in internal/agent/testdata/). Any accidental change to a description or schema fails the test and drops a `.got` file to diff. It's regenerated deliberately with:

```
go test ./internal/agent -run Golden -update
```

The test comment states the motivation: an accidental change to a description or a schema changes model behavior and throws away every prompt cache. Relatedly, role *switches* don't rewrite the system prompt either — a reminder is prefixed to the next user message (internal/agent/role.type.go), so the pinned bytes stay valid across switches.

## calls
search_code map[pattern:stable.*tool|tool.*stable]
semantic_search map[query:tool definitions stable per role prefix frozen toolset]
read_file map[path:internal/agent/definitions_test.go]
run_command map[args:[internal/agent/testdata] binary:ls]
read_file map[end_line:210 path:internal/agent/tools.go start_line:110]
know_write map[claim:Tool definitions sent to the model are kept stable per role two ways: (1) defsFor() caches the per-role list with sync.Once in l.r…
know_write map[claim:Built once per role via sync.Once; golden-file test pins bytes per role. id:n3 kind:fact path:internal/agent/tools.go]
know_write map[claim:Tool definitions sent to the model are byte-stable per role: defsFor() builds each role's list once (sync.Once over l.roleDefs[slo…
know_write map[claim:Tool definitions sent to the model are byte-stable per role: defsFor() (tools.go:150-177) builds each role's list once via sync.On…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793476031592879412"
author a2 armin session "bench-1793476031592879412"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/roles topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/roles/definition-stability fact inferred a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are byte-stable per role: defsFor() (tools.go:150-177) builds each role's list once via sync.Once over l.roleDefs[slot], filtering ToolIDs through offered(); TestDefinitionsGolden (definitions_test.go:26-58) byte-pins the marshaled JSON against testdata/definitions{,.minion,.copilot,.advisor}.json, regenerated with `go test ./internal/agent -run Golden -update`. Motivation: any description/schema drift changes model behavior and kills the prompt cache; role switches avoid rewriting the system prompt by prefixing a reminder instead."
  evidence file "internal/agent/tools.go:150-177" 83dfa246
  evidence file "internal/agent/definitions_test.go:26-58" a888bf44

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools.go:150-177 internal/agent/definitions_test.go:26-58"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence file "internal/agent/tools.go:110-210" ebcefe62

