How does armin save chat sessions to disk?

right true, 80331 prompt (65024 cached), 1232 completion, $0.01199, 8 rounds, 9 tools

## answer
Sessions live in `internal/session` (session.go) and are stored as JSON Lines files.

**Location and naming**
- Directory: `<XDG_DATA_HOME or ~/.local/share>/armin/sessions/` (session.go:55-69)
- One file per session, named `<id>.jsonl` where the id is a random UUID v4 (`NewID`, session.go:47-53). `pathFor` rejects any id containing path separators.
- Legacy files from before ids existed (one per project, `<dir>-<hash>.jsonl`) still load — their id becomes the file name (session.go:4-14, `parseMeta` at session.go:308).

**On-disk format**
- JSON Lines, deliberately not one big JSON document: the first line is a `Meta` object (id, project root, model, title, created/updated timestamps — session.type.go:10-18), followed by one line per `llm.Message`.

**Two write paths** (both in session.go):
- `Append` (session.go:190-226) — the per-turn autosave path. A `persisted` counter tracks how many leading messages are already on disk; only `history[persisted:]` is opened with `O_APPEND` and written, so each turn is O(1) amortized instead of re-serializing the whole history. If `persisted` is 0 it falls back to a full `Save` (which also lays down the metadata line).
- `Save` (session.go:118-152) — a full rewrite (`O_TRUNC` + meta line + all messages), used for infrequent whole-history writes: `/save`, `/title` resync, the exit flush.

**Where it's called**
- REPL autosaves after each turn via `maybeAutosave` → `session.Append` (repl.go:394-410); a `Save` on exit/rename (repl.go:419) and `/save` (repl/sessions.go:65). The REPL tracks `persisted` (repl.type.go:59-62) and must reset it to 0 when history is mutated in place by `/clear` or `/compact`, since `Append` only assumes appends.
- The web front end does the same after every turn when autosave is on (web.go:111).

**Security**: the sessions dir is created `0o700` and files `0o600`, since sessions contain the user's code and pasted text (session.go:130-141).

**Reading back**
- `Load` (session.go:222-270) resolves an id or a unique id prefix (`resolve`, session.go:272-303), unmarshals the meta line, then one message per line, and counts messages into `Meta.Messages`.
- `List` scans the directory and reads only the first line of each file for the `-resume` menu / `/sessions`.

## calls
project_manifest map[]
list_directory map[path:internal/session]
search_code map[file_type:go pattern:sessions]
read_file map[path:internal/session/session.go]
outline_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.type.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
search_code map[file_type:go pattern:session\.Append|session\.Save]
read_file map[end_line:320 path:internal/session/session.go start_line:220]

## tree after

