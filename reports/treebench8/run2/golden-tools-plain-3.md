When the golden definitions test fails, where does it write the new output so I can diff it?

right true, 55099 prompt (50176 cached), 722 completion, $0.00492, 6 rounds, 6 tools

## answer













It writes a **`.got`** file next to the golden, so you diff:

```
diff internal/agent/testdata/definitions.json internal/agent/testdata/definitions.json.got
```

That's `checkGolden` in `internal/agent/definitions_test.go:77-78` — on a mismatch it writes the actual bytes to `golden + ".got"` (paths are relative to the package dir, so under `internal/agent/testdata/`). The failure message tells you exactly that: `tool definitions changed; diff testdata/definitions.json{,.got}`.

Two things worth knowing:

- The per-role files pinned at `definitions_test.go:54` get their own `.got` copies too (e.g. `testdata/<role>.json.got`), so if one of those fails, same pattern applies.
- If you've *intended* the change, don't hand-edit the golden — run with the flag the test declares (`definitions_test.go:19`): `go test ./internal/agent/ -run TestToolDefinitions -update` (whatever the test is named), which rewrites the `testdata/definitions*.json` files directly.

## calls
search_code map[pattern:golden]
search_code map[file_type:go pattern:definitions\.json]
search_code map[file_type:go pattern:os\.CreateTemp|WriteFile.*definitions|\.new\.json]
search_code map[file_type:go pattern:func checkGolden]
read_file map[end_line:95 path:internal/agent/definitions_test.go start_line:60]
read_file map[end_line:20 path:internal/agent/definitions_test.go start_line:1]

## tree after

