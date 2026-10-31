How does armin save chat sessions to disk?

right false, 187668 prompt (163584 cached), 2241 completion, $0.02514, 12 rounds, 14 tools

## answer
Sessions live in `internal/session` (session.go), with the REPL/web front ends as the callers. Key facts:

**Format & location**
- One JSONL file per session: `<XDG_DATA_HOME or ~/.local/share>/armin/sessions/<id>.jsonl`, where `id` is a random UUID v4 (`NewID`, session.go:45). The dir is `0o700`, files `0o600` — owner-only, since sessions contain the user's code.
- JSON Lines, deliberately not one big JSON doc (package doc, session.go:1-22): line 1 is the `Meta` header (id, project root, model, model-written title, created/updated timestamps, session.type.go:10-18); each subsequent line is one `llm.Message` (`writeAll`, session.go:178).
- Legacy files from before ids existed (`<dir>-<hash>.jsonl`) still list and load; their id becomes the file name (`parseMeta`, session.go:308).

**Two write paths**
- `Save` (session.go:141): truncating whole-file rewrite — used for infrequent, whole-history writes: `/save`, rename/resync, and the exit flush via `REPL.SaveNow` (repl.go:412-420).
- `Append` (session.go:188): the per-turn autosave path. Opens with `O_APPEND` and writes only `history[*persisted:]`, then advances the `persisted` cursor. This makes autosaving after every turn O(1) amortized instead of re-serializing the ever-growing history each time. If `persisted == 0` it falls back to a full `Save` (which also lays down the metadata line).
- Commands that mutate history in place (`/clear`, `/compact`) must reset `persisted` and do a full `Save` via `resyncSession` (repl/sessions.go:38-48), or the next Append would miss the rewrite or duplicate lines.
- Autosave triggers: after each turn (repl.go:317, host.go:149), and from a few commands that update meta state like `/title`, `/effort`, notes (sessions.go:251, effort.go:71, notes.go:78,147).

**Reading back**
- `Load` (session.go:222) parses the first line as `Meta`, then the rest as messages; accepts a full id or a unique prefix (`resolve`).
- `List` (session.go:331) reads only each file's first line and counts the remaining lines for the message count — it never parses the messages themselves.
- `Delete` removes the file and returns the `Meta` so the caller can clean up the session's scratchpad.

Done — answer above, and recorded [n2] and [n3] in the knowledge tree for next time.

## calls
search_code map[pattern:sessions]
project_manifest map[]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
search_code map[path:internal/repl pattern:session\.(Save|Append)]
read_file map[path:internal/session/session.type.go]
read_file map[end_line:427 path:internal/session/session.go start_line:220]
search_code map[path:internal/repl pattern:maybeAutosave|fullSave|flagAutosave|autosave]
search_code map[pattern:NewID|SaveNow]
know_write map[claim:Each chat session is one JSONL file <uuid>.jsonl under <XDG_DATA_HOME|~/.local/share>/armin/sessions/: a Meta line (id, root, mode…
know_write map[claim:Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL.SaveNow). session.Appe…
know_write map[claim:Each chat session is one JSONL file <uuid>.jsonl under <XDG_DATA_HOME|~/.local/share>/armin/sessions/: a Meta line (id, root, mode…
know_write map[claim:Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL.SaveNow). session.Appe…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474423248051177"
author a2 armin session "bench-1793474423248051177"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSONL file <uuid>.jsonl under <XDG_DATA_HOME|~/.local/share>/armin/sessions/: a Meta line (id, root, model, title, timestamps) then one JSON line per llm.Message. Dir 0o700, files 0o600. Not keyed by project — the project root is inside the Meta line."
  evidence file "internal/session/session.go:1-110" 0a192a81
  evidence file "internal/session/session.type.go:10-18" e03daea8

n3 sessions/writing fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Writing: session.Save truncates and rewrites the whole file (infrequent: /save, rename, exit flush via REPL.SaveNow). session.Append is the per-turn autosave path — O_APPEND, writes only history[persisted:], advances the cursor, falls back to Save when persisted==0; commands that rewrite history in place (/clear, /compact) must resync with a full Save."
  derived-from n2
  evidence file "internal/session/session.go:141-220" 030c805e
  evidence file "internal/repl/repl.go:394-420" 6110c0d4
  evidence file "internal/repl/sessions.go:32-74" 5a123697

n4 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go internal/session/session.go:1-220 internal/session/session.go:220-427 internal/session/session.type.go"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/session/session.go:220-427" 572f50fc
  evidence session "internal/session/session.type.go" 00000000

