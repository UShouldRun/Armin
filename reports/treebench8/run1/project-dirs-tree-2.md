How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right false, 105102 prompt (85504 cached), 1297 completion, $0.01486, 9 rounds, 11 tools

## answer
All three live under `<XDG_DATA_HOME or ~/.local/share>/armin/<kind>/<project key>` (`session.ProjectDataDir`, internal/session/session.go:88), and the project key is the only non-obvious part:

**Project key** — `ProjectKey` (internal/session/session.go:75): the project's base directory name plus a `-` and the first 8 hex chars of the SHA-256 of its absolute path, e.g. `myproject-1a2b3c4d`. So two checkouts of a repo called "api" at different paths don't collide.

**Per kind:**
- **Memory**: `~/.local/share/armin/memory/<project key>` (project memories); user memories are shared at `<data>/memory/user` (internal/think/prompt.go:165,184)
- **Scratchpad**: `~/.local/share/armin/scratch/<project key>/<session ID>/` — one subfolder per session (internal/think/prompt.go:175; files are named things like `plan`, `thinking`, matching the scratchpad tools)
- **Knowledge tree**: `~/.local/share/armin/know/<project key>/`, holding the graph's `graph.snap`/`graph.log` files (internal/think/know.go:25)

Note the `armin-test` build renames the top folder, so a test build keeps `~/.local/share/armin-test/...` apart from the installed one (internal/build/build.go:3-6, docs/guide/install.md:84).

For contrast, saved *sessions* aren't project-scoped folders — each is one file named by its UUID under the sessions data dir (internal/session/session.go:1-5 per the header comment).

## calls
semantic_search map[count10:10 query:per-project folder naming memory scratchpad knowledge tree directory path]
semantic_search map[count:10 query:per-project folder naming memory scratchpad knowledge tree directory path]
search_code map[file_type8go:go pattern:os.UserHomeDir|XDG|\.armin|armin-]
search_code map[file_type:go pattern:UserHomeDir|filepath.Join.*armin|\.armin]
search_code map[pattern:scratchpad|knowledge tree]
read_file map[end_line:200 path:internal/think/prompt.go start_line:150]
search_code map[file_type:go path:internal/notes pattern:scratches|scratch|memories|trees|sha256|Hash]
search_code map[file_type:go path:internal/session pattern:ProjectDataDir|func.*DataDir]
search_code map[file_type:go path:internal/session pattern:ProjectKey|sha256|hex\.]
search_code map[file_type:go pattern:ProjectDataDir\(\"tree\"|\"tree\", root|trees]
search_code map[file_type:go pattern:ProjectDataDir\(]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300077083"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:69 session.type.go:10 session.go:124 repl/repl.type.go:59 repl/repl.go:398 host.go:149 web/web.go:111 session.go:227"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-200" 8ab68438
  evidence file "internal/session/session.go:200-300" c6a635e8
  evidence session "internal/session/session.type.go" 00000000

