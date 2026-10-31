When the golden definitions test fails, where does it write the new output so I can diff it?

right true, 28908 prompt (22400 cached), 354 completion, $0.00253, 3 rounds, 3 tools

## answer


Let me find that golden definitions test in your codebase.



</invoke>





It writes it **right next to the golden file**: on mismatch `checkGolden` writes the new bytes to `testdata/<name>.got` and the failure message tells you exactly what to run (`internal/agent/definitions_test.go:75-79`).

So for a per-role failure you diff:

```
diff internal/agent/testdata/definitions.minion.json{,.got}
```

(same pattern for `definitions.json`, `definitions.copilot.json`, `definitions.advisor.json`)

If you've confirmed the change is intentional, regenerate with:

```
go test ./internal/agent -run Golden -update
```

## calls
search_code map[pattern:golden]
search_code map[pattern:definitions.*test]
read_file map[path:internal/agent/definitions_test.go]

## tree after

