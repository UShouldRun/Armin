When the golden definitions test fails, where does it write the new output so I can diff it?

right true, 13497 prompt (10240 cached), 299 completion, $0.00232, 2 rounds, 1 tools

## answer
When it fails, `checkGolden` writes the new output right next to the golden file with a `.got` suffix (`definitions_test.go:72-75`). So for each role you get:

- `internal/agent/testdata/definitions.json.got`
- `internal/agent/testdata/definitions.minion.json.got`, `.copilot.json.got`, `.advisor.json.got`

and the failure message tells you exactly what to diff: `diff testdata/definitions.json{,.got}`.

Separately, if you've reviewed the diff and want to *accept* the change as the new baseline, that's the `-update` flag, which overwrites the goldens in place instead of writing `.got` files (`definitions_test.go:8-9`, `63-66`):

```
go test ./internal/agent -run Golden -update
```

## calls
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

