How does armin save chat sessions to disk?

right true, 140824 prompt (106240 cached), 132305 completion, $0.34724, 10 rounds, 13 tools

## answer
There's a dedicated `internal/session/` package. Let me look at it.

Let me read the full file in chunks.

Let me check the type definitions and how the REPL wires this up.

Let me confirm the autosave call site and exit flush.

Here's how armin persists chat sessions to disk.

## Where
- One file per session, at `~/.local/share/armin/sessions/<id>.jsonl` (honoring `XDG_DATA_HOME`), where `<id>` is an RFC 4122 v4 UUID. `Dir()` at `internal/session/session.go:62`, `pathFor()` at `:101`.
- The session directory is `MkdirAll`'d `0o700` and each file created `0o600` — sessions hold your code and pastes, so they're readable by you alone (`Save`, `session.go:130-139`).

## Format: JSON Lines, not one JSON doc
This is the key design choice (`session.go` package doc, lines 1-19):
- **Line 1** is the `Meta` struct (`session.type.go:9`): `id`, `root` (project dir), `model`, `title` (model-written), `created_at`, `updated_at`.
- **Each subsequent line** is one `llm.Message`.

Because it's append-only JSONL, armin can write *only the new messages* instead of re-serializing the whole growing history every turn. A full rewrite would be O(n) I/O per turn / O(n²) total; appending is O(1) amortized per turn / O(n) total.

## Two write paths
- **`Save`** (`session.go:117`) — a full snapshot: opens with `O_TRUNC` and rewrites the meta line plus all messages. Used for infrequent whole-history moments: `/save`, a rename (`/title`), and the exit flush.
- **`Append`** (`session.go:180`) — the hot path. It writes only `history[*persisted:]` (messages added since the last write) in `O_APPEND` mode, then advances `*persisted`. If `*persisted == 0` nothing's written yet this run, so it falls back to a full `Save` to lay down the meta line. The `persisted` counter lives on the REPL (`repl.type.go:59`); anything that mutates history in place (`/clear`, `/compact`) must reset it to 0 or the next autosave would silently skip.

## How the REPL drives it
- On startup the REPL mints the session: `r.Meta = session.Meta{ID: session.NewID(), Root: sb.Root, Model: client.Model}` (`repl.go:113`).
- **Autosave after each turn**: the main loop calls `r.maybeAutosave()` (`repl.go:317`), which is gated on `flagAutosave` and calls `session.Append(&r.Meta, r.History, &r.persisted)` (`repl.go:398-404`). `-no-save` turns this flag off.
- **Full `Save`** is also called from `/save` (`sessions.go:65`), `/title`'s resync, and other whole-history mutations (`sessions.go:44,65`).

## Reading back
`Load(id)` (`session.go:223`) resolves an id (or a unique prefix via `resolve`), reads the first line as `Meta`, and unmarshals the rest as messages into a `Saved`. `List()` (`session.go:326`) stays cheap by reading only each file's first line and counting the rest without parsing the messages. Legacy pre-UUID files (`<dir>-<hash>.jsonl`) still load, using the filename as the id.

So: a UUID-named JSONL file under `$XDG_DATA_HOME/armin/sessions`, meta line first, one message per line, appended incrementally each turn and fully rewritten only on explicit saves or exit.

That's the full picture. In short: JSONL under `~/.local/share/armin/sessions`, meta line first, one message per line, appended incrementally each turn (`Append`) and fully rewritten only on explicit saves and exit (`Save`).

## calls
project_manifest map[]
search_code map[pattern:session]
list_symbols map[query:session]
list_directory map[path:internal/session]
read_file map[path:internal/session/session.go]
search_code map[path:internal/session pattern:func \(?\*?Session]
search_code map[path:internal/session pattern:func .*]
read_file map[end_line:220 path:internal/session/session.go start_line:1]
read_file map[path:internal/session/session.type.go]
search_code map[path:internal/repl pattern:session\.(Save|Append|NewID|Meta)]
search_code map[path:internal/repl pattern:maybeAutosave|fullSave|OnExit|maybeSave]
know_write map[]
know_write map[claim:armin persists each chat session as one JSONL file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (dir 0700, file 0600). Line 1 is …

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793474539443299607"
author a2 armin session "bench-1793474539443299607"

n1 session topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 session/persistence concept inferred a1 born 2026-10-31 seen 2026-10-31
  claim "armin persists each chat session as one JSONL file at $XDG_DATA_HOME/armin/sessions/<uuid>.jsonl (dir 0700, file 0600). Line 1 is Meta (id, root, model, title, created/updated); each later line is one llm.Message. Save (O_TRUNC full rewrite) is for /save, /title and exit flush; Append is the per-turn hot path writing only history[*persisted:] in O_APPEND. The REPL calls maybeAutosave() each turn (flagAutosave, off with -no-save)."
  evidence file "internal/session/session.go:1-230" 2cda16cd
  evidence file "internal/session/session.type.go:1-28" 336538f9
  evidence session "internal/repl/repl.go:113,317,398-404" 00000000

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-does-armin-save file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go internal/session/session.go:1-220 internal/session/session.type.go"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-220" 3a162043
  evidence session "internal/session/session.type.go" 00000000

