How does armin save chat sessions to disk?

right true, 72511 prompt (62720 cached), 965 completion, $0.01002, 7 rounds, 9 tools

## answer
Sessions are stored as append-only JSONL files in `~/.local/share/armin/sessions/<id>.jsonl` (the dir honors `XDG_DATA_HOME`, see `DataDir`, internal/session/session.go:47). One file per session, with:

- **First line**: a JSON `Meta` object — session id (random UUID v4 from `NewID`), project root, model, title, created/updated timestamps (internal/session/session.type.go:8).
- **Remaining lines**: one JSON-serialized `llm.Message` per line — the full chat history, including tool calls/results.

How writes happen:

- **Per turn** — `Append` (session.go:180) opens the file `O_APPEND` and writes only `history[*persisted:]`, i.e. just the new messages since the last write, then advances `*persisted`. If `*persisted` is 0 it does a full `Save` first to lay down the meta line.
- **Whole-history rewrite** — `Save` (session.go:117) truncates and rewrites everything; used only for infrequent cases: a rename, `/save`, and the exit flush.
- **Mutation** — commands that rewrite history in place (`/clear`, `/compact`) must `Save` and reset `*persisted` themselves, since append assumes only additions.

Security: sessions contain the user's code, so the folder is created/chmodded `0700` and files `0600` (session.go:127-135). IDs are validated in `pathFor` against path traversal (no `/`, `\`, leading `.`).

Reading: `Load` parses the meta line + all messages (supports a unique-id prefix match via `resolve`), `List` reads only the first line of each file and counts the rest without parsing messages, and `Latest` finds the newest session for a given project root. Deletion is `Delete` (backed by `/sessions rm`).

## calls
search_code map[pattern:session]
project_manifest map[]
list_directory map[path:internal/session]
read_file map[path:docs/components/memory-and-sessions.md]
read_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.type.go]
outline_file map[path:internal/session/session.go]
search_code map[path:internal/session/session.go pattern:^func]
read_file map[end_line:220 path:internal/session/session.go start_line:100]

## tree after

