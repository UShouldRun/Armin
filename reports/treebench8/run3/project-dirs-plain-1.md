How does armin save chat sessions to disk?

right true, 94645 prompt (82944 cached), 1279 completion, $0.01300, 9 rounds, 11 tools

## answer
Sessions live in `~/.local/share/armin/sessions/<id>.jsonl` (`id` is a v4 UUID) and are saved by the `internal/session` package with two complementary paths:

**Format — JSON Lines, append-only**
- First line: a `Meta` object (`id`, `root` project dir, `model`, `title`, `created_at`/`updated_at` — `internal/session/session.type.go:10`)
- Then one JSON line per `llm.Message`. This is deliberately not one big JSON document, so appends are cheap: O(1) amortized per turn instead of re-serializing the whole history (the rationale is in the package doc, `internal/session/session.go:1-17`).

**Two write paths**
- `Append(m, history, &persisted)` (`session.go:181`) — the per-turn autosave. Opens the file with `O_APPEND | O_WRONLY` and writes only `history[persisted:]`, then bumps `persisted`. If `persisted == 0` (nothing on disk yet) it falls back to a full `Save` to lay down the metadata line. The callers (`internal/repl/repl.go:395` `maybeAutosave`, `internal/web/web.go:111`) track the `persisted` offset themselves; operations that rewrite history in place (`/clear`, `/compact`) must `Save` and reset the counter instead.
- `Save(m, history)` (`session.go:122`) — full rewrite with `O_TRUNC`, used for infrequent whole-history writes: rename, `/save`, and the exit flush (`REPL.SaveNow`, `repl.go:409`). It stamps `UpdatedAt` and writes the metadata line fresh.

**Where it happens**
- `REPL.maybeAutosave()` runs after each turn unless `-no-save` cleared the autosave flag; on exit `SaveNow` does the final full write. Same pattern in the web frontend.
- `Load(id)` (`session.go:221`) reads it back: first line as meta, rest as messages; `-resume` / `/resume` accept a full id or a unique prefix.

**Safety**
- The folder is `0700` and files `0600` (sessions hold pasted code); `Save` even re-chmods the dir to tighten pre-existing folders.
- `pathFor` rejects ids containing `/`, `\` or leading dots to keep files inside the sessions dir.
- Legacy per-project files (`<dir>-<hash>.jsonl`) from before ids existed still list and load; their filename is treated as the id.

## calls
semantic_search map[query:save chat session to disk persist conversation]
search_code map[pattern:session]
find_files map[pattern:*session*]
read_file map[path:docs/components/memory-and-sessions.md]
read_file map[path:internal/session/session.go]
outline_file map[path:internal/session/session.go]
read_file map[end_line:200 path:internal/session/session.go start_line:1]
read_file map[path:internal/session/session.type.go]
search_code map[file_type:go pattern:Append\(]
read_file map[end_line:420 path:internal/repl/repl.go start_line:385]
read_file map[end_line:300 path:internal/session/session.go start_line:200]

## tree after

