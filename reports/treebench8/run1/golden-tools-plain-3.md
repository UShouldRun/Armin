When the golden definitions test fails, where does it write the new output so I can diff it?

right true, 25902 prompt (20672 cached), 410 completion, $0.00248, 3 rounds, 3 tools

## answer







It writes it next to the golden file with a `.got` suffix. In `internal/agent/definitions_test.go:77`, on mismatch `checkGolden` does:

```go
os.WriteFile(golden + ".got", got, 0o644)
t.Fatalf("tool definitions changed; diff testdata/%s{,.got}", name)
```

So for each failing file you get e.g. `internal/agent/testdata/definitions.json.got` (and `definitions.<role>.json.got` per role), and the failure message tells you exactly what to run: `diff testdata/definitions.json{,.got}`. To adopt the new output, re-run with the `-update` flag (definitions_test.go:18), which rewrites the `testdata/definitions*.json` files.

## calls
search_code map[pattern:golden]
search_code map[pattern:expected.*output|update.*expected|rewrite.*expected]
search_code map[file_type:go pattern:definitions.*\.json|\.got]

## tree after

