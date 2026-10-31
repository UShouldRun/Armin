How does armin name the per-project folders it keeps for memory, the scratchpad and the knowledge tree?

right true, 47949 prompt (35568 cached), 813 completion, $0.00223, 5 rounds, 5 tools

## answer


All three kinds go through the same helper, `session.ProjectDataDir(kind, root)`, which builds `<data>/armin/<kind>/<project key>` (session.go:86-100):

- **Project key** — `ProjectKey` (session.go:75-84) = the project root's base name + `"-"` + the first 8 hex chars of the SHA-256 of its absolute path, e.g. `armin-1a2b3c4d`. The hash is there so two checkouts with the same base name (say two `api` folders) don't collide.
- **Kind segment** — the caller picks it:
  - memory → `ProjectDataDir("memory", root)` (think/prompt.go:165)
  - scratchpad → `ProjectDataDir("scratch", root)` (think/prompt.go:175; also reused in repl/sessions.go:196 when deleting a session's scratchpad)
  - knowledge tree → `ProjectDataDir("know", root)` (think/know.go:25)

So for a project at `/home/u/armin`, the three folders are `…/armin/memory/armin-<8hex>`, `…/armin/scratch/armin-<8hex>`, and `…/armin/know/armin-<8hex>`, where `…` is the data dir (`$XDG_DATA_HOME` or `~/.local/share`, per DataDir, session.go:48-59). Only sessions (`sessions/<id>.jsonl`) skip this per-project naming.

## calls
search_code map[path:internal/session pattern:MkdirAll|filepath.Join]
semantic_search map[count:8 query:per-project directory naming for memory scratchpad knowledge tree folder]
search_code map[file_type:go path:internal pattern:scratchpad|knowledge|memory]
read_file map[end_line:113 path:internal/session/session.go start_line:72]
search_code map[file_type:go pattern:ProjectDataDir\(]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519012784"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "session.go:48-69 session.go:121-133 session.type.go:9-21 session.go:148-170 session.go:118 repl/sessions.go:65 session.go:181 repl.type.go:58-62 repl.go:393-407 repl.go:113 repl.go:404 session.go:224-275 session.go:327 session.go:103"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:100-220" 6fefcd9c
  evidence session "internal/session/session.type.go" 00000000

