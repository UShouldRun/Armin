How does armin save chat sessions to disk?

right true, 95318 prompt (85248 cached), 1511 completion, $0.01346, 10 rounds, 12 tools

## answer
Sessions live in the `internal/session` package: each conversation is one **JSON-Lines file**, not a single JSON document.

**Location** — `Dir()` (session.go:62): `$XDG_DATA_HOME/armin/sessions/`, falling back to `~/.local/share/armin/sessions/`. Each file is `<id>.jsonl` where the id is a random UUIDv4 (`NewID`, session.go:37).

**Format** —
- Line 1: a `Meta` object (`id`, `root`, `model`, `title`, `created_at`, `updated_at`) — see `session.type.go:10-19`
- Then one JSON line per `llm.Message`

**Two write paths** (the design is explained in the package doc, session.go:1-17):
- `Save` (session.go:117) — writes a fresh whole-file snapshot with `O_TRUNC`, used for infrequent full rewrites: `/save`, renames, and the exit flush. Creates the dir with `0o700` and the file with `0o600` since sessions contain user code.
- `Append` (session.go:180) — the per-turn path. It keeps a `*persisted` count and writes only `history[*persisted:]` with `O_APPEND`, so each turn is O(1) amortized instead of re-serializing the ever-growing history. Callers that mutate history in place (`/clear`, `/compact`) call `Save` and reset the counter instead.

**Read paths** — `Load` (session.go:223) accepts a full id or a unique prefix via `resolve` (session.go:272); `List` (session.go:326) only parses each file's first `Meta` line and counts the rest without decoding messages; `Latest` (session.go:387) finds the most recently updated session for a given project root.

Legacy files from before ids existed (named `<dir>-<hash>.jsonl`) still load — their id is just the file name (session.go:304-307).

## calls
know_find map[query:chat sessions saved disk persistence storage]
semantic_search map[query:save chat session to disk]
search_code map[file_type_specific:true pattern:session]
list_directory map[depth:2]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
outline_file map[path:internal/session/session.go]
search_code map[path:internal/session pattern:func ]
read_file map[path:internal/session/session.type.go]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
know_write map[claim:Each chat session is one JSON-Lines file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (or ~/.local/share/armin/sessions). Line 1 …
know_write map[claim:Each chat session is one JSON-Lines file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (or ~/.local/share/armin/sessions). Line 1 …

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245271763"
author a2 armin session "bench-1793473527245271763"

n1 session topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 session/storage fact checked a1 born 2026-10-31 seen 2026-10-31
  claim "Each chat session is one JSON-Lines file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (or ~/.local/share/armin/sessions). Line 1 is a Meta (id, root, model, title, timestamps), then one JSON line per llm.Message. Save() rewrites the whole file (rename, /save, exit flush); Append() writes only new messages using a *persisted counter (O(1) per turn). Load() reads a file by id or unique prefix; List() reads only first lines."
  evidence file "internal/session/session.go:46-210" bdfbdb50
  evidence file "internal/session/session.type.go:1-28" 336538f9

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "session.go:62 session.go:37 session.type.go:10-19 session.go:1-17 session.go:117 session.go:180 session.go:223 session.go:272 session.go:326 session.go:387 session.go:304-307"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 87ec9031
  evidence session "internal/session/session.type.go" 00000000

