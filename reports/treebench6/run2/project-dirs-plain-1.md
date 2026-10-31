How does armin save chat sessions to disk?

right true, 85984 prompt (75520 cached), 1119 completion, $0.01169, 8 rounds, 9 tools

## answer
Sessions are append-only JSON-Lines files in the `session` package (`internal/session/`). Here's how it works:

**Location & format**
- One file per session at `~/.local/share/armin/sessions/<id>.jsonl` (honors `XDG_DATA_HOME`), where `id` is a random UUID v4 (`NewID`, session.go:41). The dir is `0700`, files `0600` — sessions contain the user's code, so owner-only.
- The on-disk format is JSON Lines, **not** one big JSON doc (package doc, session.go:1-22):
  - Line 1: a `Meta` header — id, project root, model, model-written title, created/updated timestamps (session.type.go:10-18).
  - One line per `llm.Message` after that.

**Why JSONL**
Append is only possible because of this format. The whole point (session.go:1-20): a per-turn autosave that re-serialized the entire history would be O(n) per turn / O(n²) total I/O; appending is O(1) amortized / O(n) total.

**Two write paths**
- `Append` (session.go:202): writes only `history[*persisted:]` — messages added since the last write — then bumps `persisted` to `len(history)`. If `persisted == 0` it falls back to a full `Save` (which also writes the meta line). The REPL calls it from `maybeAutosave` after each turn (repl.go:397-411), and web.go:111 does the same.
- `Save` (session.go:124): truncates and rewrites the whole file. Reserved for infrequent whole-history writes: renames/`/title`, `/save`, and the exit flush (`REPL.SaveNow`, repl.go:415). Callers that mutate history in place (`/clear`, `/compact`) must `Save` and reset `persisted` themselves, since `Append`'s pointer is only valid for pure appends.

**Reading back**
`Load` (session.go:222) opens the file, unmarshals line 1 as `Meta`, then each subsequent line as a message; a scanner buffer capped at 8 MiB handles long lines. `resolve` lets you load by a unique id prefix. Legacy pre-UUID files (named `<dir>-<hash>.jsonl`, one per project) still load — their id is just the filename (session.go:302-315). `-no-save` disables the autosave flag entirely.

Related but separate: the scratchpad lives per project/session under `~/.local/share/armin/scratch/<project>/<session>/` (docs/armin.1:417).

## calls
search_code map[pattern:session]
project_manifest map[]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
read_file map[end_line:320 path:internal/session/session.go start_line:220]
read_file map[path:internal/session/session.type.go]
find_callers map[name:Append]
read_file map[end_line:420 path:internal/repl/repl.go start_line:390]

## tree after

