How does armin save chat sessions to disk?

right true, 158295 prompt (137216 cached), 1531 completion, $0.02039, 13 rounds, 15 tools

## answer
Sessions live in `internal/session` and are stored as **JSON Lines files, one per session**:

**Location & naming**
- Stored in `~/.local/share/armin/sessions/` (honors `XDG_DATA_HOME`), directory and files created `0700`/`0600` so only you can read them, since sessions hold your code and pasted text (`internal/session/session.go:38-56`, `:116-126`).
- Each file is `<uuid>.jsonl` — the id is an RFC 4122 v4 UUID generated at session start (`NewID`, `internal/session/session.go:42-50`). Legacy sessions without ids keep the old `<dir>-<hash>.jsonl` naming.

**On-disk format**
- First line: a `Meta` JSON object — id, project root, model, model-written title, created/updated timestamps (`internal/session/session.type.go:8-17`).
- Every subsequent line: one `llm.Message` JSON-encoded. This is deliberately append-only JSONL rather than one big JSON document, so per-turn saves don't re-serialize the whole history (`internal/session/session.go:6-16`).

**How it's written**
- `Save` (`session.go:116`) rewrites the whole file — used for infrequent full-history writes: `/save`, `/title` rename, the exit flush, and any command that mutates history in place (`/clear`, `/compact`) via `resyncSession` (`internal/repl/sessions.go:44`).
- `Append` (`session.go:181`) is the per-turn path: it opens the file `O_APPEND` and writes only `history[persisted:]` — the messages added since the last save — then advances a `persisted` counter. That's O(1) amortized per turn instead of O(n²) total disk I/O.
- The REPL calls `maybeAutosave` after each turn (`internal/repl/repl.go:398`) unless `-no-save` is set; the web server does the same per turn (`internal/web/web.go:98-115`). The first save of a run (persisted == 0) falls back to a full `Save` to lay down the metadata line.

**Reading back**
- `Load` (`session.go:228`) resolves an id or a unique prefix, reads the meta line, then unmarshals each message line; `List` (`session.go:327`) reads just the first line of each file and counts the rest to build the `/sessions` menu — it never parses the messages themselves.

## calls
semantic_search map[query:save chat session to disk]
list_directory map[depth:1]
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=search_code>
<parameter=pattern>
scratchpad pattern:session]
search_code map[file_type:go pattern:scratchpad]
search_code map[pattern:session]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
outline_file map[path:internal/session/session.go]
read_file map[end_line:160 path:internal/session/session.go start_line:1]
search_code map[path:internal/session pattern:func (Append|Read|List)]
read_file map[end_line:260 path:internal/session/session.go start_line:161]
read_file map[path:internal/session/session.type.go]
search_code map[file_type:go path:internal pattern:session.Append|session.Save|Autosave]
search_code map[path:internal/repl pattern:func (r \*REPL) maybeAutosave]
search_code map[path:internal/repl pattern:maybeAutosave]

## tree after

