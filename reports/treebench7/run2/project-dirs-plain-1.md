How does armin save chat sessions to disk?

right true, 78421 prompt (69376 cached), 1590 completion, $0.01203, 9 rounds, 12 tools

## answer
Sessions live as JSONL files in `~/.local/share/armin/sessions/` (honoring `XDG_DATA_HOME`, `session.DataDir`/`Dir`, internal/session/session.go:47-70). One file per session: `<uuid>.jsonl` — the id is an RFC 4122 v4 UUID, and `pathFor` rejects ids containing `/` or `\` so nothing escapes the dir (session.go:101-112).

**File format** — plain JSON, one object per line:
- Line 1: the `Meta` header (id, project root, model, title, created/updated timestamps)
- Each subsequent line: one `llm.Message`

`writeAll` (session.go:147-177) encodes exactly that, updating `UpdatedAt` as it goes.

**Two write paths**, chosen by cost:
- `Save` (session.go:117-145) — a full rewrite: opens with `O_TRUNC` and re-encodes the whole history. Used for infrequent whole-history writes: rename, `/save`, and the exit flush (`REPL.SaveNow`, internal/repl/repl.go:410).
- `Append` (session.go:180-221) — the per-turn path. The REPL tracks a `persisted` counter; each turn it opens the file with `O_APPEND` and encodes only `history[persisted:]`, then advances the counter. So autosave after every turn is O(new messages) instead of re-writing the ever-growing history. If `persisted == 0` it falls back to a full `Save` (which lays down the meta line). Callers that mutate history in place (`/clear`, `/compact`) must `Save` and reset the counter themselves.

The per-turn call is `REPL.maybeAutosave` (internal/repl/repl.go:399), and the web front end uses the same `session.Append` (internal/web/web.go:111).

**Reading back:**
- `Load` (session.go:223) resolves the id — or a unique prefix of it, via `resolve` — then decodes the meta line and each message line.
- `List` (session.go:326) only reads each file's first line and counts the rest; it never parses the messages.
- Legacy files from before ids existed (named `<dir>-<hash>.jsonl`) still load: `parseMeta` fills the missing id from the filename (session.go:305-314).

**Security:** the directory is created `0700` and files `0600` — the comment in `Save` notes sessions hold the user's code and pastes, so they're readable by the owner alone.

## calls
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=project_manifest>
</function>
</tool_call>
<tool_call>
<function=project_mani…
project_manifest map[]
outline_file map[path:internal/session]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
read_file map[path:internal/session/session.type.go]
search_code map[file_type:go path:internal/session/session.go pattern:func \(.*\) .*(Save|SaveToFile|Persist|Flush)]
search_code map[file_type:go path:internal/session/session.go pattern:^func |json|os\.]
read_file map[end_line:112 path:internal/session/session.go start_line:46]
read_file map[end_line:220 path:internal/session/session.go start_line:113]
find_callers map[name:Append]
read_file map[end_line:415 path:internal/repl/repl.go start_line:395]

## tree after

