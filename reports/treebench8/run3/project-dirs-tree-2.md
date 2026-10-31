How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 71847 prompt (51648 cached), 982 completion, $0.00430, 5 rounds, 7 tools

## answer













All three live under the XDG data dir (`~/.local/share/armin`, or `$XDG_DATA_HOME/armin`) in a shared shape, one folder per concern:

```
~/.local/share/armin/<kind>/<project key>/
```

- **Memory** — `memory/<project key>` (prompt.go:165)
- **Scratchpad** — `scratch/<project key>` (prompt.go:175)
- **Knowledge tree** — `know/<project key>` (think/know.go:25), holding `graph.snap` / `graph.log` inside

So the folders differ only in the middle segment; the leaf name is identical and comes from `ProjectKey` (session/session.go:75-84): the project root's **base directory name** plus a **short hash** — the first 8 hex chars of the SHA-256 of the absolute root path:

```go
return filepath.Base(abs) + "-" + hex.EncodeToString(sum[:])[:8], nil
```

e.g. a checkout of `~/code/api` at that path becomes `api-3f9c2b1a`. The hash exists so two projects with the same base name in different locations don't collide (session.go:73-74). The whole scheme is wired up by `ProjectDataDir(kind, root)` (session.go:88-98), which joins `<data>/armin/<kind>/<key>`.

Note this is separate from chat sessions, which aren't per-project folders at all — they're individual `<uuid>.jsonl` files under `<data>/armin/sessions/` (session.go:63-68) [n2].

## calls
search_code map[pattern:per-project|projectDir|ProjectDir]
search_code map[file_type:go pattern:armin]
search_code map[path:internal pattern:func ProjectKey|ProjectKey\(]
search_code map[file_type:go path:internal pattern:scratch|know|memory]
read_file map[end_line:170 path:internal/session/session.go start_line:55]
search_code map[path:internal/think pattern:ProjectDataDir|projectMem|scratch|know]
find_references map[name:ProjectDataDir path:internal]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031592753586"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "session.go:41-48 session.go:125-131 session.go:152-168 session.go:1-19 session.go:114 repl/sessions.go:44 repl/repl.go:419 session.go:180 repl/repl.go:394-406 web/web.go:111 repl/repl.type.go:59-62 session.go:219"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-120" 34ca5f31
  evidence file "internal/session/session.go:120-260" 7ff0dcb4

