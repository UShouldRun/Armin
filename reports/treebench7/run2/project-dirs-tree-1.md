How does armin save chat sessions to disk?

right true, 138871 prompt (120576 cached), 1489 completion, $0.01823, 10 rounds, 13 tools

## answer
Each session is one JSONL file, `<uuid>.jsonl` under `~/.local/share/armin/sessions/` (or `$XDG_DATA_HOME/armin/sessions`), stored by the `internal/session` package:

**Format** (`internal/session/session.type.go:8-27`)
- First line: a `Meta` object — id (random v4 UUID, `NewID` at session.go:45), project root, model, title, created/updated timestamps.
- Then one JSON line per `llm.Message`. The "Messages" count isn't stored; `List`/`Load` count the lines.

**Where it lives**: `Dir()` at session.go:73 joins DataDir + `sessions/`. `pathFor` (session.go:107) names the file `<id>.jsonl` and rejects ids with path separators.

**Writing** — two paths:
- `Save` (session.go:142): truncates and rewrites the whole file. Used for infrequent full rewrites — `/save` (repl/sessions.go:65), session rename/resync (repl/sessions.go:44), and the exit flush via `SaveNow` (repl.go:411-419), which skips if autosave is off or the user never said anything (`hasTurns`).
- `Append` (session.go:193): the per-turn path. The REPL keeps `r.persisted` = how many leading messages are already on disk (repl.type.go:59), and after each turn `maybeAutosave` (repl.go:397) appends only `history[persisted:]` in `O_APPEND` mode, then advances the counter. That's O(1) amortized per turn instead of O(n²) disk I/O over the session. If `persisted` is 0 it falls back to a full `Save`. Callers that mutate history in place (`/clear`, `/compact`) must `Save` and reset `persisted` themselves. The web UI does the same per-turn `Append` (web/web.go:111).

**Safety**: the directory is created 0700 and files 0600, since sessions contain the user's code and pastes (session.go:148-153).

**Reading**: `Load` (session.go:222) takes an id or a unique id prefix (`resolve`), parses the meta line then the messages with an 8 MiB line cap; `List` only reads first lines + counts. Legacy pre-UUID files (`<dir>-<hash>.jsonl`, one per project) still list/load — their id is the filename (session.go:277).

## calls
search_code map[pattern:session]
project_manifest map[]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.type.go]
outline_file map[path:internal/session/session.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
read_file map[end_line:427 path:internal/session/session.go start_line:221]
find_references map[name:session.Append]
find_references map[name:session.Save]
know_write map[claim:Each chat session is one JSONL file &lt;uuid&gt;.jsonl under &lt;XDG_DATA_HOME|~/.local/share&gt;/armin/sessions/: a Meta line (id…
read_file map[end_line:427 path:internal/repl/repl.go start_line:385]
search_code map[pathrepl:/internal/repl pattern:SaveNow|hasTurns|autosave]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474471068804107"
author a2 armin session "bench-1793474471068804107"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSONL file &lt;uuid&gt;.jsonl under &lt;XDG_DATA_HOME|~/.local/share&gt;/armin/sessions/: a Meta line (id, root, model, title, timestamps), then one JSON line per llm.Message."
  evidence file "internal/session/session.go:1-120" 995fba5e
  evidence file "internal/session/session.type.go:8-27" 4e667c53

n3 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.type.go:8-27 session.go:45 session.go:73 session.go:107 session.go:142 repl/sessions.go:65 repl/sessions.go:44 repl.go:411-419 session.go:193 repl.type.go:59 repl.go:397 web/web.go:111 session.go:148-153 session.go:222 session.go:277"
  evidence file "internal/repl/repl.go:385-427" 88a7b8f1
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/session/session.go:221-427" 2fa881e5
  evidence session "internal/session/session.type.go" 00000000

