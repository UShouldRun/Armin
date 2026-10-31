Where are a project's knowledge tree files stored, and what are they called?

right true, 120213 prompt (100800 cached), 1278 completion, $0.01005, 10 rounds, 10 tools

## answer























A project's knowledge tree lives in the data directory, one subdirectory per project:

**Location** — `~/.local/share/armin/know/<project key>/` (or `$XDG_DATA_HOME/armin/know/...`; a test build uses `armin-test` as the name instead of `armin`).

- `openKnow` opens the tree under `<data>/armin/know/<project key>` — internal/think/know.go:21-30.
- The directory comes from `session.ProjectDataDir("know", root)` — internal/session/session.go:88-96 — which joins `<data dir>` + kind + key.
- The **project key** is the project's base directory name plus the first 8 hex chars of the SHA-256 of its absolute path, e.g. `armin-5d4162d4` (internal/session/session.go:73-83), so two checkouts with the same name don't collide.

**File names** — the tree is a directory, not a single file, with three files inside (internal/know/store.go):

- `graph.snap` — the mmap'd binary snapshot of the graph, rewritten by `Compact`
- `graph.log` — the append-only op log replayed on top of the snapshot
- `lock` — a flock file; while another armin holds it, the tree opens read-only

Related but not per-project: the **learner tree** (what the *user* knows) lives at `~/.local/share/armin/learner/` (internal/think/know.go:85-88), also with the same `graph.snap`/`graph.log` pair. Both are backed by the same engine, package `know`.

## calls
run_command map[binary:ls]
search_code map[pattern:knowledge.*tree|know.*\.json|\.know]
search_code map[file_type:go path:internal/know pattern:\.json]
list_directory map[depth:1 path:internal/know]
read_file map[path:internal/know/store.go]
search_code map[file_type:go pattern:know\.Open\(]
search_code map[file_type:go pattern:openKnow|knows\b|/knows|knows/]
search_code map[file_type:go pattern:func ProjectDataDir|func projectKey|local/share]
read_file map[end_line:140 path:internal/session/session.go start_line:50]
search_code map[file_type:go path:internal/think pattern:func openLearner]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300077083"
author a2 armin session "bench-1793475824139503071"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:69 session.type.go:10 session.go:124 repl/repl.type.go:59 repl/repl.go:398 host.go:149 web/web.go:111 session.go:227"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-200" 8ab68438
  evidence file "internal/session/session.go:200-300" c6a635e8
  evidence session "internal/session/session.type.go" 00000000

n3 sessions/where-are-a-project's file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/think/know.go:21-30 internal/session/session.go:88-96 internal/session/session.go:73-83 internal/think/know.go:85-88"
  evidence session "internal/know/store.go" 00000000
  evidence file "internal/session/session.go:50-140" 70fb3301

