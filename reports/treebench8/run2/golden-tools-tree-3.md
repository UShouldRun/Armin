When the golden definitions test fails, where does it write the new output so I can diff it?

right true, 25198 prompt (19200 cached), 311 completion, $0.00371, 3 rounds, 3 tools

## answer
On a failure, it writes the new output next to the golden file in the package dir: `internal/agent/testdata/definitions.<role>.json.got` (e.g. `definitions.json.got`), and the test message tells you exactly how to compare: `diff testdata/<name>{,.got}`. That's `internal/agent/definitions_test.go:77-78`.

If you've decided the change is intended, regenerate the goldens instead with `go test ./internal/agent -run Definitions -update` (the `-update` flag, `definitions_test.go:18-19`), which rewrites the `testdata/definitions*.json` files in place.

## calls
search_code map[pattern:golden]
find_files map[pattern:definitions.json]
search_code map[path:internal/agent pattern:testdata]

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

