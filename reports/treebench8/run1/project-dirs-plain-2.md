How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 50678 prompt (40704 cached), 855 completion, $0.00780, 5 rounds, 6 tools

## answer
All three use one shared scheme, built by `ProjectKey` / `ProjectDataDir` in `internal/session/session.go:71-99`:

- **The folder name (the "project key"):** the base name of the project root's absolute path, plus `-`, plus the first **8 hex chars of the SHA-256 of the absolute path** — e.g. `armin-1f3c9a02`. The hash keeps two checkouts with the same name (say two `api` dirs) from colliding. `ProjectKey` is at session.go:74-83.

- **The full path:** `<data>/armin/<kind>/<project key>`, where `<data>` is `$XDG_DATA_HOME/armin` (default `~/.local/share/armin`), and `ProjectDataDir(kind, root)` (session.go:88) joins it. The three kinds:
  - **memory** → `…/armin/memory/<key>` — used by `openNotes` (internal/think/prompt.go:165)
  - **scratch** → `…/armin/scratch/<key>/<session-id>/` — scratchpad, per session (internal/think/prompt.go:175; the session subfolder is deleted with `/sessions rm` in internal/repl/sessions.go:196)
  - **know** → `…/armin/know/<key>` — the knowledge tree (internal/think/know.go:25)

So on a default system you get e.g. `~/.local/share/armin/memory/armin-1f3c9a02`, `…/scratch/armin-1f3c9a02/<uuid>/`, and `…/know/armin-1f3c9a02`.

## calls
search_code map[pattern:scratchpad]
semantic_search map[query:per-project folder naming memory scratchpad knowledge tree]
search_code map[pattern:ProjectKey]
semantic_search map[query:knowledge tree per-project storage folder naming]
read_file map[end_line:110 path:internal/session/session.go start_line:50]
search_code map[path:internal pattern:ProjectDataDir\(]

## tree after

