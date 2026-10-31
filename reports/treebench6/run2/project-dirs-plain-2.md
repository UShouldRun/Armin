How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 87196 prompt (75008 cached), 1477 completion, $0.01288, 9 rounds, 12 tools

## answer
All three live under armin's data directory (`$XDG_DATA_HOME/armin`, defaulting to `~/.local/share/armin`), one `<kind>/<project-key>` folder each — see `internal/session/session.go:85-96` (`ProjectDataDir`). The kinds are:

- **memory** → `…/armin/memory/<key>` (opened at `internal/think/prompt.go:145`)
- **scratch (scratchpad)** → `…/armin/scratch/<key>` (`internal/think/prompt.go:155`)
- **know (knowledge tree)** → `…/armin/know/<key>` (`internal/think/know.go:25`)

The `<key>` (the "project key") is `<basename>-<hash>`: the base name of the project's absolute path plus the first 8 hex chars of its SHA-256 — e.g. `armin-1a2b3c4d` — so two projects both called `api` don't collide (`ProjectKey`, `internal/session/session.go:75-83`).

One wrinkle: the scratchpad is session-scoped inside that folder. For UUID sessions, each session gets its own subfolder named by its session id, and deleting a session removes its scratchpad folder too (`internal/repl/sessions.go:194-198`).

## calls
run_command map[args_bash:[armin] binary:which]
run_command map[args_bash:[~] binary:ls]
run_command map[args:[~] binary:echo]
project_manifest map[]
run_command map[args:[-rn scratchpad internal] binary:rg]
search_code map[path:internal/know pattern:armin\.data|\.armin|dataDir|DataDir]
read_file map[path:internal/notes/notes.go]
search_code map[file_type:go pattern:LocalShare|HOME|\.local]
read_file map[path:internal/session/session.go]
read_file map[end_line:120 path:internal/session/session.go start_line:1]
search_code map[file_type:go pattern:ProjectDataDir\(]
search_code map[file_type:go path:internal/know pattern:DataDir|ProjectKey|session\.]

## tree after

