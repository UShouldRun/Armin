When the golden definitions test fails, where does it write the new output so I can diff it?

right true, 13341 prompt (11616 cached), 292 completion, $0.00067, 2 rounds, 1 tools

## answer


On failure it writes the new output next to the golden file, with a `.got` suffix — so diff:

```
diff testdata/definitions.json{,.got}
```

That's `internal/agent/definitions_test.go:77-78`: `os.WriteFile(golden + ".got", got, 0o644)`, then the test fails with the message `tool definitions changed; diff testdata/<name>{,.got}`. The same happens for the per-role files (`definitions.minion.json.got`, etc.).

If you *intend* the change, don't diff — just regenerate in place with:

```
go test ./internal/agent -run Golden -update
```

which writes `testdata/definitions*.json` directly (`-update` flag, line 19).

## calls
read_file map[path:internal/agent/definitions_test.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793475801301765362"
author a2 armin session "bench-1793475801301765362"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/tools topic inferred a1 born 2026-10-31 seen 2026-10-31

n3 agent/tools/golden-pinning decision checked a1 born 2026-10-31 seen 2026-10-31
  claim "Tool definitions sent to the model are pinned byte-for-byte by the golden-file test TestDefinitionsGolden (internal/agent/definitions_test.go): it dumps l.Definitions() per role plus the bare ToolDefinitions() to testdata/definitions[.<role>].json for minion/copilot/advisor and fails on any diff, writing the new bytes to <golden>.got. Regenerate deliberately with `go test ./internal/agent -run Golden -update`. Per-role pinning exists because a changed tool schema changes model behavior and invalidates prompt caches."
  evidence file "internal/agent/definitions_test.go:22-80" 2eb3c6bd
  evidence file "internal/agent/tools.go:103-147" 156309bd

n4 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n5 sessions/how-does-armin-make file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/definitions_test.go"
  evidence session "internal/agent/definitions_test.go" 00000000

