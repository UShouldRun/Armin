How does armin save chat sessions to disk?

right true, 55123 prompt (44288 cached), 1101 completion, $0.00891, 7 rounds, 8 tools

## answer
Sessions live in `internal/session` (`internal/session/session.go`). Key points:

**Where on disk**
- One JSONL file per session, named by a UUID: `<XDG_DATA_HOME|~/.local/share>/armin/sessions/<id>.jsonl` (`Dir()`, `pathFor()`, session.go:53-117).
- The id is a random v4 UUID (`NewID()`, session.go:41-48).
- The dir is created `0o700`, files opened `0o600` — sessions hold the user's code/pasted text, so readable by them alone (session.go:145-149).

**Format**
- JSON Lines, not one JSON document: a metadata line first (project root, model, title, timestamps), then one line per message (`writeAll`, session.go:174-195). This is what makes append-only writes cheap.

**Two write paths**
- `Save` (session.go:134-172): full rewrite — used for infrequent whole-history writes: `/save`, rename, and the exit flush.
- `Append` (session.go:203-233): writes only `history[*persisted:]` (messages added since last save), then advances `*persisted`. If `*persisted == 0` it falls back to a full `Save` to lay down the metadata line. Callers that mutate history in place (`/clear`, `/compact`) must `Save` and reset `*persisted` themselves.

**Who calls it**
- After each turn the REPL calls `maybeAutosave` → `session.Append(&r.Meta, r.History, &r.persisted)` (`internal/repl/repl.go:317, 398-408`), gated on the autosave flag (`-no-save` turns it off).
- `SaveNow` (repl.go:412) does a final full `Save` on exit, unless saving is off or there were no user turns.
- The web front end uses the same `Append` per turn (`internal/web/web.go:111`).

The whole point of the split: a per-turn autosave that rewrote the whole file each time would be O(n²) total I/O over an n-turn session; append keeps it O(1) amortized per turn.

## calls
semantic_search map[query:save chat session to disk persist session]
search_code map[file_typego:</function>
</tool_call>
<tool_call>
<function=search_code>
<parameter=pattern>
WriteFile.*session|json.Marshal.*[Mm]essage …
search_code map[file_typeinternalgo: pathgointernal: pattern:Save|persist]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
find_references map[name:session.Append]
search_code map[path:internal/repl pattern:maybeAutosave|SaveNow|flagAutosave]

## tree after

