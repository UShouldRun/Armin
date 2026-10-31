Where are a project's knowledge tree files stored, and what are they called?

right true, 102426 prompt (92352 cached), 1158 completion, $0.00872, 9 rounds, 12 tools

## answer

















</invoke>






A project's knowledge tree is stored outside the project itself, under armin's data directory:

**Location** — `~/.local/share/armin/know/<project key>/` (honors `XDG_DATA_HOME`, and `armin-test` uses `armin-test` in place of `armin`). The `<project key>` is the project's base name plus an 8-char hash of its absolute path, so two same-named checkouts don't collide (see `session.ProjectDataDir` / `session.ProjectKey` in internal/session/session.go:88, and `openKnow` in internal/think/know.go:24 which calls it with kind `"know"`).

**Files inside that directory** (see internal/know/store.go):
- `graph.snap` — the snapshot: a 96-byte header, then the node/edge/evidence/author arrays and string arena, mmap'd on load (written on `Compact`)
- `graph.log` — the append-only operation log replayed over the snapshot at open
- `lock` — an flock file; a second armin on the same project opens the tree read-only while it's held

The human-readable form (what `-know dump` prints and `-know edit` opens in `$EDITOR`) isn't a stored file — it's rendered on demand from the graph by internal/know/dump.go.

## calls
search_code map[pattern:knowledge.?tree]
search_code map[pattern:knowledge_tree]
search_code map[file_type:go path:internal/know pattern:\.md|\.txt|filename|FileName]
semantic_search map[query:knowledge tree file storage location on disk per project]
run_command map[args:[internal/know] binary:ls]
search_code map[file_type:go path:internal/know pattern:Join\(.*"|Save|Load]
search_code map[file_type:go path:internal/know pattern:func Open|knowDir|KnowDir]
search_code map[file_type:go pattern:know\.Open|knowdir|know/]
read_file map[end_line:40 path:internal/think/know.go start_line:1]
find_definition map[name:ProjectDataDir]
search_code map[file_type:go path:internal/session pattern:func ProjectDataDir|func.*DataDir|local/share]
read_file map[end_line:130 path:internal/session/session.go start_line:40]

## tree after

