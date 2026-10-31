Which file pins the advisor's tool list, and what command regenerates it?

right true, 40732 prompt (31488 cached), 591 completion, $0.00616, 4 rounds, 5 tools

## answer
The advisor's tool list is pinned by the golden file **`internal/agent/testdata/definitions.advisor.json`**, checked byte-for-byte by `TestDefinitionsGolden` in `internal/agent/definitions_test.go:26-66` (the test does `l.SetRole(RoleAdvisor)` and compares the marshaled `l.Definitions()` to that file).

To regenerate it on purpose:

```
go test ./internal/agent -run Golden -update
```

The `-update` flag (definitions_test.go:18-20) makes `checkGolden` rewrite all four golden files instead of failing. Without it, a mismatch writes `definitions.advisor.json.got` next to the golden and tells you to `diff testdata/definitions.advisor.json{,.got}`.

Worth knowing why: the test's comment says an accidental change to a description or schema "changes model behavior and throws away every prompt cache" — the per-role pinning is exactly what makes tool definitions byte-stable across role switches [n3]. (The list is also asserted semantically by `role_test.go:26-34` — no `edit_file`/`write_file`/`run_program`, but `read_file`/`web_search`/`run_tests` must be present.)

## calls
search_code map[path:internal/agent pattern:advisor]
semantic_search map[query:advisor tool list pinned generated]
search_code map[pattern:definitions.advisor.json]
find_files map[pattern:*advisor*]
read_file map[path:internal/agent/definitions_test.go]

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

