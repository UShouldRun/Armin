How does armin save chat sessions to disk?

right true, 37230 prompt (28160 cached), 813 completion, $0.00638, 4 rounds, 6 tools

## answer
Sessions are append-only JSON Lines files, managed in `internal/session/session.go`:

**Format & location**
- One file per session: `~/.local/share/armin/sessions/<id>.jsonl` (honors `XDG_DATA_HOME`; `DataDir`/`Dir`, session.go:58-76). The id is a random UUID (`NewID`, session.go:45-51).
- Line 1 is a metadata line (project dir, model, title, timestamps); each subsequent line is one `llm.Message`, encoded with `json.Encoder` — so the file is JSONL, not a single JSON doc (session.go:5-13).
- Files are mode `0o600` (owner-only).

**Two write paths**
- `Append` (session.go:180) — the hot path. The REPL keeps a `persisted` counter in `repl` (repl.type.go:59-61); after every turn, `maybeAutosave` (repl.go:394-405) calls `Append`, which opens the file with `O_APPEND|O_WRONLY` and writes only `history[*persisted:]`, then advances the counter. First call (persisted == 0) does a full `Save` to lay down the metadata line. This keeps autosave O(1) amortized instead of O(n²) total I/O.
- `Save` (session.go:117) — a full rewrite, used only for whole-history changes: `/save`, `/title` (rename), `/clear` or `/compact` (which mutate history in place, so they `Save` and reset `persisted` — see `internal/repl/sessions.go:33-35`), and the exit flush.

**Resume side**
- `Load` reads the JSONL back; legacy sessions from before ids existed (`<dir>-<hash>.jsonl`) still list and load, with that filename as the id (package doc, session.go:16-17).
- Per-session scratchpads live separately at `~/.local/share/armin/scratch/<project>/<session>/`, removed together when you `/sessions rm`.

## calls
search_code map[pattern:session]
semantic_search map[query:persist chat session to disk save history]
find_files map[path:internal pattern:session*.go]
search_code map[path:internal/session pattern:Append|O_APPEND|flock|Sync]
read_file map[end_line:115 path:internal/session/session.go start_line:1]
find_references map[name:Append path:internal/repl]

## tree after

