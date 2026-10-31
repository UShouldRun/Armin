How does armin save chat sessions to disk?

right true, 85343 prompt (75264 cached), 1214 completion, $0.01182, 9 rounds, 11 tools

## answer
Sessions live in `internal/session` (docs/components/memory-and-sessions.md) and are stored as **append-only JSONL files**, one per session.

**Location & format**
- Directory: `~/.local/share/armin/sessions/` — honoring `$XDG_DATA_HOME` via `DataDir()`/`Dir()` (internal/session/session.go:47,62). Folder 0700, files 0600 since sessions hold the user's pasted code.
- File name is the session id (a random UUID v4, `NewID()`) plus `.jsonl`. `pathFor` rejects any id containing `/`, `\`, or starting with `.` (session.go:101).
- Format: first line is a JSON `Meta` (id, root, model, title, created_at, updated_at), then one JSON-encoded `llm.Message` per line (`writeAll`, session.go:147). `List` only reads the first line of each file plus counts lines — it never parses the messages (session.go:324-326).

**Write paths**
- `Save` (session.go:117) truncates and rewrites the whole file — used only for infrequent whole-history writes: renames, `/save`, and the exit flush.
- `Append` (session.go:180) is the per-turn path: it opens with `O_APPEND` and writes only `history[*persisted:]` — messages added since the last write — then bumps a `persisted` counter the caller keeps. If nothing is on disk yet (`persisted == 0`) it falls back to a full `Save` to lay down the meta line. Callers that mutate history in place (`/clear`, `/compact`) must `Save` and reset `persisted` themselves.
- The REPL calls `Append` after each turn (internal/repl/repl.go:404); the web frontend does the same (internal/web/web.go:111).

**Read/delete**
- `Load` (session.go:223) accepts an id or a unique id prefix and rebuilds `Saved` (Meta + full history).
- `Delete` (session.go:410) returns the Meta so the caller can clean up the session's scratchpad; that's what `/sessions rm` uses.

So the design is cheap append-per-turn, no re-serialization of the full history on every message.

## calls
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=semantic_search>
<parameter=query>
save chat session to disk, persist convers…
search_code map[pattern:session]
semantic_search map[query:save chat session to disk, persist conversation]
list_directory map[path:internal/session]
read_file map[path:docs/components/memory-and-sessions.md]
read_file map[path:internal/session/session.type.go]
outline_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.go]
search_code map[path:internal/session pattern:^func]
read_file map[end_line:220 path:internal/session/session.go start_line:101]
find_callers map[name:Append]

## tree after

