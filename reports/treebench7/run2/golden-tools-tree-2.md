Which file pins the advisor's tool list, and what command regenerates it?

right true, 42672 prompt (28608 cached), 510 completion, $0.00456, 4 rounds, 5 tools

## answer


The advisor's tool list is pinned in **`internal/agent/testdata/definitions.advisor.json`** — a golden file that `TestDefinitionsGolden` (`internal/agent/definitions_test.go:26`) compares byte-for-byte, so any accidental change to a tool's description or schema fails the test (and would throw away the prompt cache).

To regenerate it:

```
go test ./internal/agent -run Golden -update
```

This runs the same test with the `-update` flag (`definitions_test.go:18-19`, `66-68`), rewriting all `testdata/definitions*.json` files (the per-role ones plus the full `definitions.json`) from the current `l.Definitions()`. If it fails without the flag, it leaves a `.got` file so you can diff what changed.

## calls
search_code map[pattern:pins the tool list|regenerat.*tool]
search_code map[pattern:advisor]
search_code map[path:internal/agent pattern:update]
run_command map[args:[definitions internal/agent] binary:find]
read_file map[end_line:80 path:internal/agent/definitions_test.go start_line:1]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474471069409311"
author a2 armin session "bench-1793474485300815539"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-make file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:26 definitions_test.go:44 definitions_test.go:61 definitions_test.go:19 definitions_test.go:22-25"
  evidence session "internal/agent/definitions_test.go" 00000000

n3 sessions/which-file-pins-the file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go:26 definitions_test.go:18-19"
  evidence file "internal/agent/definitions_test.go:1-80" 80cf8b0b

