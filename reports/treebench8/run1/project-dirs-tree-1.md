How does armin save chat sessions to disk?

right true, 88783 prompt (73472 cached), 1229 completion, $0.01275, 9 rounds, 11 tools

## answer
Sessions live in `internal/session` and are stored as one JSON Lines file per session.

**Where**: `$XDG_DATA_HOME/armin/sessions/<id>.jsonl` (default `~/.local/share/armin/sessions/`, see `Dir()` in `internal/session/session.go:69`). The id is a UUID v4 (`NewID`); legacy pre-UUID files named `<project>-<hash>.jsonl` are still loadable. Directory is `0700`, files `0600` — sessions contain the user's code.

**On-disk format**: the first line is the `Meta` line (id, project root, model, model-written title, created/updated timestamps — `session.type.go:10`), then one JSON line per `llm.Message`. JSONL rather than one JSON document is deliberate: it makes incremental appending possible.

**Two write paths** (`session.go:124` and `:173`):
- **`Save`** — full snapshot; opens the file with `O_TRUNC` and re-encodes meta + whole history. Used for infrequent whole-history writes: `/save`, rename (`/rename`), resync after `/clear`/`/compact`, and the exit flush.
- **`Append`** — the per-turn autosave. Opens with `O_APPEND` and writes only `history[persisted:]`, then advances the `persisted` cursor. The caller (REPL or web front end) tracks `persisted` as "how many leading messages are already on disk" (`repl/repl.type.go:59`). This keeps autosaving O(1) amortized per turn instead of O(n²) total I/O.

**Callers**: the REPL autosaves after each turn via `maybeAutosave` (`repl/repl.go:398`, gated on the `-autosave`/flagAutosave state, called from the run loop and on exit in `host.go:149`), plus after notes/effort changes; the web front end calls `Append` on the same path (`web/web.go:111`). Commands that mutate history in place (`/clear`, `/compact`) reset `persisted = 0` and do a full `Save`, so the append cursor can't point at lines that no longer match memory.

**Read side**: `Load` (`session.go:227`) accepts a full id or a unique prefix, reads the meta line, then unmarshals each subsequent line into the history.

## calls
search_code map[file_type-go: pattern:session]
project_manifest map[]
list_directory map[path:internal/session]
search_code map[file_type:go path:internal/session pattern:WriteFile|O_CREATE|json\.Encode|json\.Marshal]
read_file map[path:internal/session/session.go]
outline_file map[path:internal/session/session.go]
read_file map[end_line:200 path:internal/session/session.go start_line:1]
read_file map[end_line:300 path:internal/session/session.go start_line:200]
read_file map[path:internal/session/session.type.go]
search_code map[file_type:go pattern:session\.Append|session\.Save\(]
search_code map[file_type:go path:internal/repl pattern:maybeAutosave|fullSave|autosave]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300077083"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:69 session.type.go:10 session.go:124 repl/repl.type.go:59 repl/repl.go:398 host.go:149 web/web.go:111 session.go:227"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-200" 8ab68438
  evidence file "internal/session/session.go:200-300" c6a635e8
  evidence session "internal/session/session.type.go" 00000000

