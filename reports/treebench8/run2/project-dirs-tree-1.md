How does armin save chat sessions to disk?

right true, 73911 prompt (64256 cached), 1180 completion, $0.01066, 8 rounds, 9 tools

## answer
armin stores sessions as JSONL files in an XDG data dir, one file per session. Key points:

**Where & what** (`internal/session/session.go`)
- `DataDir()` → `$XDG_DATA_HOME` (else `~/.local/share`), sessions live in `<data>/armin/sessions/` via `Dir()` (session.go:48-69).
- Each session is `<uuid>.jsonl`, written with `0600` permissions and the directory `0700` — sessions hold the user's code, so they're user-only readable (session.go:121-133).
- File format is line-delimited JSON: a first-line `Meta` (id, root, model, title, created/updated timestamps, session.type.go:9-21), then one JSON object per `llm.Message` in history (`writeAll`, session.go:148-170).

**Two write paths**
- `Save(m, history)` (session.go:118) — truncates and rewrites the whole file. Used for infrequent whole-history writes: `/save` (repl/sessions.go:65), a rename/resync, and the exit flush.
- `Append(m, history, &persisted)` (session.go:181) — opens the file with `O_APPEND` and writes only the messages added since the last save, tracked by the `persisted` counter on the REPL (repl.type.go:58-62). This is the per-turn autosave, making autosave amortized O(1) instead of re-writing the ever-growing history (repl.go:393-407). If `persisted == 0` it falls back to a full `Save`.

**Lifecycle in the REPL**
- New session gets a random UUID v4 id at start (`session.NewID()`, repl.go:113); autosave runs after every turn via `maybeAutosave` → `session.Append` (repl.go:404).
- `-resume` / `Latest(root)` find a session: `resolve` matches an id or a unique prefix, `Load` reads back `Meta` + full history (session.go:224-275, 388). `List` only reads each file's first line and counts the rest, never parsing messages (session.go:327).
- The session id is validated in `pathFor` to reject path-traversal ids (session.go:103).

Callers that mutate history in place (`/clear`, `/compact`) must `Save` and reset `persisted` themselves, since `Append` assumes the on-disk prefix is intact.

## calls
search_code map[file_typeA:go pattern:session]
project_manifest map[]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.type.go]
outline_file map[path:internal/session/session.go]
search_code map[path:internal/session pattern:^func ]
read_file map[end_line:220 path:internal/session/session.go start_line:100]
search_code map[path:internal/repl pattern:session\.(Save|Append|Latest|NewID)]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519012784"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "session.go:48-69 session.go:121-133 session.type.go:9-21 session.go:148-170 session.go:118 repl/sessions.go:65 session.go:181 repl.type.go:58-62 repl.go:393-407 repl.go:113 repl.go:404 session.go:224-275 session.go:327 session.go:103"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:100-220" 6fefcd9c
  evidence session "internal/session/session.type.go" 00000000

