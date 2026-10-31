How does armin save chat sessions to disk?

right true, 113770 prompt (100352 cached), 1955 completion, $0.01660, 9 rounds, 13 tools

## answer
Armin stores each session as a single JSONL file in its data directory: `~/.local/share/armin/sessions/<uuid>.jsonl` (or `$XDG_DATA_HOME/armin/sessions`, session.go:55-70). The UUID is a random RFC 4122 v4 id (session.go:39-48).

**On-disk format** (session.go:1-220):
- **Line 1**: the `Meta` — session id, project root, model, a model-written title, created/updated timestamps.
- **Lines 2+**: one `llm.Message` per line.

It's JSON Lines specifically so appends are cheap — you don't have to re-serialize the whole ever-growing history each turn (package doc, session.go:8-17).

**Two write paths:**
- `Save(m, history)` (session.go:117): opens the file with `O_TRUNC` and writes a fresh full snapshot (metadata line + every message). Used for infrequent whole-history writes: `/save` (repl/sessions.go:65), the exit flush `SaveNow` (repl/repl.go:410-420), and resync after `/clear`/`/compact` (repl/sessions.go:44) — those mutate history in place, so appending from the old offset would be wrong.
- `Append(m, history, &persisted)` (session.go:180): opens with `O_APPEND` and writes only `history[*persisted:]`, then bumps the counter. This is the per-turn autosave: the REPL calls it after each turn in `maybeAutosave` (repl/repl.go:398-408), and the web UI does the same (web/web.go:111). It's O(1) amortized per turn instead of O(n²) total I/O. If `persisted == 0` (first save of the run) it falls back to a full `Save`.

Permissions: the sessions dir is created 0o700 and files 0o600, since sessions hold the user's code (session.go:126-132).

Reading back: `Load(id)` accepts a full id or a unique prefix; `List`/`Latest(root)` enumerate the directory; legacy pre-id files named `<dir>-<hash>.jsonl` still load, with the id taken from the filename (session.go:313).

## calls
know_find map[query:chat session save persist disk storage]
project_manifest map[]
list_directory map[path:internal/session]
search_code map[file_type:go path:internal/session pattern:Save|Persist|WriteFile|\.json]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
find_callers map[name:session.Append]
find_callers map[name:session.Save]
find_references map[name:Append path:internal/repl]
search_code map[file_type:go pattern:session\.(Save|Append)]
read_file map[end_line:427 path:internal/session/session.go start_line:395]
read_file map[end_line:421 path:internal/repl/repl.go start_line:390]
know_write map[claim:Sessions persist as one JSONL file per session at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (default ~/.local/share/armin/session…
know_write map[claim:Sessions persist as one JSONL file per session at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (default ~/.local/share/armin/session…

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106232441"
author a2 armin session "bench-1793473478106232441"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/persistence fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Sessions persist as one JSONL file per session at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (default ~/.local/share/armin/sessions), dir 0o700, file 0o600. First line is the Meta (id, project root, model, title, timestamps), then one line per llm.Message. Save() rewrites the whole file (used by /save, exit flush SaveNow, /clear, /compact resync); Append() opens O_APPEND and writes only history[*persisted:] (per-turn autosave in the REPL maybeAutosave and web, O(1) amortized). Callers that mutate history in place must Save + reset persisted to 0. Load/List/resolve support ids or unique prefixes; legacy pre-id files <dir>-<hash>.jsonl still load, id taken from the filename."
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/repl/repl.go:394-420" 6110c0d4
  evidence file "internal/repl/sessions.go:33-67" ed55b8c5
  evidence file "internal/web/web.go:109-113" 2be50de5

n3 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "session.go:55-70 session.go:39-48 session.go:1-220 session.go:8-17 session.go:117 repl/sessions.go:65 repl/repl.go:410-420 repl/sessions.go:44 session.go:180 repl/repl.go:398-408 web/web.go:111 session.go:126-132 session.go:313"
  evidence file "internal/repl/repl.go:390-421" 4bec4d83
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence file "internal/session/session.go:395-427" 57dd6f7e

