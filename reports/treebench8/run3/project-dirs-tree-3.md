Where are a project's knowledge tree files stored, and what are they called?

right true, 79925 prompt (70464 cached), 871 completion, $0.00673, 7 rounds, 7 tools

## answer















A project's knowledge tree lives under:

```
$XDG_DATA_HOME/armin/know/<project key>/   (i.e. ~/.local/share/armin/know/<project key>/)
```

- The path is built by `ProjectDataDir("know", root)` — internal/session/session.go:88 — which composes `<data>/armin/<kind>/<project key>`. The project key is `<basename>-<hash>` (basename of the project's absolute path + first 8 hex chars of its SHA-256, so two projects with the same name don't collide) — internal/session/session.go:75-83. The tree is opened there in `openKnow` (internal/think/know.go:22-32).
- Inside that directory the tree is stored as three files, managed in internal/know/store.go:
  - `graph.snap` — the snapshot (mmap-loaded on open, rewritten on compaction — store.go:92, 226)
  - `graph.log` — the append-only operation log replayed over the snapshot at open; a torn tail is cut off (store.go:124-165)
  - `lock` — a flock file; if another armin instance already holds it, this one opens the tree read-only (store.go:77-90)

## calls
semantic_search map[query:knowledge tree file persistence storage location on disk]
search_code map[pattern:knowledge]
read_file map[end_line:60 path:internal/think/know.go start_line:1]
search_code map[file_type:go path:internal/know pattern:\.json|\.armin|filename|Filename|files :=|files \[]
search_code map[file_type:go path:internal/know pattern:Open|Save|dir]
search_code map[file_type:go path:internal/know/store.go pattern:\.snap|\.log|filepath.Join|lock]
search_code map[file_type:go path:internal/session pattern:func ProjectDataDir|armin]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031592753586"
author a2 armin session "bench-1793476056997325519"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-armin-save file checked a1 born 2026-10-31 seen 2026-10-31
  claim "session.go:41-48 session.go:125-131 session.go:152-168 session.go:1-19 session.go:114 repl/sessions.go:44 repl/repl.go:419 session.go:180 repl/repl.go:394-406 web/web.go:111 repl/repl.type.go:59-62 session.go:219"
  evidence session "internal/session/session.go" 00000000
  evidence file "internal/session/session.go:1-120" 34ca5f31
  evidence file "internal/session/session.go:120-260" 7ff0dcb4

n3 sessions/where-are-a-project's file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/session/session.go:88 internal/session/session.go:75-83 internal/think/know.go:22-32 store.go:92 store.go:124-165 store.go:77-90"
  evidence file "internal/think/know.go:1-60" 8f16e11c

