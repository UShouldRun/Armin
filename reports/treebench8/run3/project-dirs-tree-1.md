How does armin save chat sessions to disk?

right true, 116193 prompt (99584 cached), 1425 completion, $0.01585, 8 rounds, 10 tools

## answer
Sessions live in `internal/session` (`session.go`, `session.type.go`), and both the REPL and the web frontend save through it:

**Where**
- One file per session: `<XDG_DATA_HOME>/armin/sessions/<id>.jsonl` (default `~/.local/share/armin/sessions/`), where `<id>` is a random UUID (`session.NewID`, session.go:41-48).
- Directory is created `0o700`, files opened `0o600` — sessions hold the user's code and pastes, so readable by them alone (session.go:125-131).

**On-disk format: JSON Lines**
- First line is the `Meta` JSON (id, project root, model, title, timestamps); then one JSON line per `llm.Message` (session.go:152-168).
- JSONL instead of one big document is deliberate: it's what makes append-only saving possible (package doc, session.go:1-19).

**Two write paths**
- `Save` (session.go:114) truncates and rewrites the whole file — used for infrequent whole-history writes: `/save`, `/title` renames, `/clear`/`/compact` resync (`resyncSession`, repl/sessions.go:44), exit flush (repl/repl.go:419).
- `Append` (session.go:180) writes only `history[*persisted:]` with `O_APPEND` and advances the counter. This is the per-turn path: `maybeAutosave` calls it after every turn in the REPL (repl/repl.go:394-406) and in web (web/web.go:111). Callers that mutate history in place must reset `persisted` to 0 or fall back to a full `Save` (repl/repl.type.go:59-62).

The doc comment explains the payoff: a REPL that rewrote the whole file each turn would do O(n) work per turn and O(n²) total I/O over an n-turn session; append is O(1) amortized per turn, O(n) total.

**Reading back**
- `Load` (session.go:219+) takes an id or a unique prefix, reads the meta line, then each message line with a scanner buffered up to 8 MiB per line. Legacy one-file-per-project sessions (`<dir>-<hash>.jsonl`, from before ids) still list and load, with that name as their id.

`-no-save` (the `autosave` flag) skips the per-turn path entirely.

## calls
search_code map[pattern:chat]
semantic_search map[query:saving chat sessions to disk persistence]
search_code map[path:/home/henrique/Code/armin pattern:resume]
search_code map[path:/home/henrique/Code/armin pattern:session]
find_files map[path:internal/session pattern:*.go]
read_file map[path:internal/session/session.go]
read_file map[end_line:120 path:internal/session/session.go start_line:1]
read_file map[end_line:260 path:internal/session/session.go start_line:120]
search_code map[path:internal/repl pattern:snapshot|session\.Append|session\.Save]
search_code map[path:internal/web pattern:session\.Append|session\.Save]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031592753586"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "session.go:41-48 session.go:125-131 session.go:152-168 session.go:1-19 session.go:114 repl/sessions.go:44 repl/repl.go:419 session.go:180 repl/repl.go:394-406 web/web.go:111 repl/repl.type.go:59-62 session.go:219"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-120" 34ca5f31
  evidence file "internal/session/session.go:120-260" 7ff0dcb4

