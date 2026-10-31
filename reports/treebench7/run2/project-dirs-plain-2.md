How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 56766 prompt (45056 cached), 857 completion, $0.00854, 5 rounds, 6 tools

## answer
All three use one shared scheme, built by `ProjectKey` / `ProjectDataDir` in `internal/session/session.go:71-100`:

**Folder name (the "project key"):** the project's base directory name + `-` + the first 8 hex chars of the SHA-256 of its absolute path — e.g. `armin-1a2b3c4d` (session.go:80-82). The hash stops two checkouts both named `api` from colliding.

**Where it lives:** `ProjectDataDir(kind, root)` returns `<data>/armin/<kind>/<project key>` (session.go:85-100), so on a default system:

| thing | kind | folder |
|---|---|---|
| memory | `memory` | `~/.local/share/armin/memory/<key>` |
| scratchpad | `scratch` | `~/.local/share/armin/scratch/<key>` |
| knowledge tree | `know` | `~/.local/share/armin/know/<key>` |

Callers: `openNotes` uses `"memory"` and `"scratch"` (`internal/think/prompt.go:165,175`); `openKnow` uses `"know"` (`internal/think/know.go:25`).

One wrinkle: the scratchpad is session-scoped inside that folder — for UUID sessions each session gets its own subfolder named by its session id, and `/sessions rm` deletes that subfolder (`internal/repl/sessions.go:194-198`).

## calls
semantic_search map[query:per-project folder names for memory scratchpad knowledge tree]
search_code map[pattern:scratchpad]
search_code map[pattern:ProjectKey]
search_code map[file_type:go path:internal pattern:sha256|fnv|hash]
read_file map[end_line:110 path:internal/session/session.go start_line:70]
search_code map[file_type:go path:internal pattern:ProjectDataDir\(]

## tree after

