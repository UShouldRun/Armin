How does armin make sure the tool definitions sent to the model don't change by accident, for each role?

right true, 49551 prompt (40448 cached), 1009 completion, $0.00798, 5 rounds, 7 tools

## answer
It pins the tool definitions with a golden-file test, and that test has one golden file **per role**. The mechanism works in two layers:

**1. Definitions are built once per role and cached**

Each `Loop` keeps a slot of pre-built definitions per role (`roleDefs [4]roleDefs` in `internal/agent/loop.type.go:109`). On first use for a role, `defsFor()` walks the tool table in fixed table order, filters by what the role is offered (`l.offered(id)`), and stores the list via `sync.Once` (`internal/agent/tools.go:149-170`). So within a session the JSON bytes sent to the model are stable — the list is never rebuilt or reordered. This matters for the local server's prompt cache: the system prompt and tool definitions are the "Fixed" part that no compaction ever removes (`internal/agent/policy.type.go:31-33`).

**2. A golden-file test pins those bytes, one file per role**

`TestDefinitionsGolden` (`internal/agent/definitions_test.go:25`) builds a deterministic Loop (a demo skill, an empty library, no memories) and compares the marshaled definitions **byte for byte** against checked-in golden files:

- `internal/agent/testdata/definitions.json` — all roles (no role) plus the bare `ToolDefinitions()`
- `internal/agent/testdata/definitions.minion.json`
- `internal/agent/testdata/definitions.copilot.json`
- `internal/agent/testdata/definitions.advisor.json`

The test's own comment states the why: "an accidental change to a description or a schema changes model behavior and throws away every prompt cache" (definitions_test.go:24-26).

The check itself is strict string equality — on a mismatch it writes out `testdata/definitions.<role>.json.got` so you can `diff testdata/%s{,.got}` and fail the test (definitions_test.go:76-79). Changing the definitions on purpose is a deliberate act: rerun with

```
go test ./internal/agent -run Golden -update
```

which rewrites the golden files (definitions_test.go:72-75).

Additionally, `TestToolDefinitionsShape` (`internal/agent/loop_test.go:19`) pins the count at 38 entries, so a tool added or dropped is caught even if the per-role golden comparison somehow missed it.

So: accidental drift in any description or schema trips the golden test for that role; only an explicit `-update` run changes what the model is told.

## calls
search_code map[path:internal pattern:tool.*defin]
semantic_search map[query:stable tool definitions per role hash cache drift]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/definitions_test.go]
search_code map[path:internal/agent pattern:roleDefs|Definitions\(\)|SetRole|defsFor]
find_files map[path:internal/agent/testdata pattern:definitions*.json]
read_file map[end_line:175 path:internal/agent/tools.go start_line:140]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519132117"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-make file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/loop.type.go:109 internal/agent/tools.go:149-170 internal/agent/policy.type.go:31-33 internal/agent/definitions_test.go:25 definitions_test.go:24-26 definitions_test.go:76-79 definitions_test.go:72-75 internal/agent/loop_test.go:19"
  evidence session "internal/agent/definitions_test.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:140-175" c9df97e0

