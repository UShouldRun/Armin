Where are a project's knowledge tree files stored, and what are they called?

right true, 127062 prompt (114880 cached), 1296 completion, $0.01049, 10 rounds, 13 tools

## answer
























The project's knowledge tree is stored in a directory keyed by project under the data dir:

```
~/.local/share/armin/know/<project>-<hash>/
```

(`~/.local/share/armin-test/know/...` for the `armin-test` install.) It's opened by `openKnow` in internal/think/know.go:23 via `session.ProjectDataDir("know", root)`.

The files in that directory (an LSM-style log-structured store, internal/know/store.go):

| File | Contents |
|---|---|
| `graph.snap` | The compacted snapshot of all nodes and edges — written on `Compact()` at shutdown (store.go:226) |
| `graph.log` | Append-only log of ops since the last snapshot (store.go:125) |
| `stamps` | The staleness-check stamps: per cited file, size/mtime/ctime/inode, written by `refreshKnow` (internal/think/stale.go:56) |
| `lock` | Lockfile so a second armin on the same project opens the tree read-only (store.go:78) |

All of the tree's content — nodes (claims, confidence, evidence, paths like `tcp/handshake`), edges, and the `cycles/` question nodes — lives inside `graph.snap` + `graph.log`, not in separate files. You can view or edit it with `armin -know dump` / `armin -know edit` (docs/guide/memory.md:74-108).

## calls
search_code map[pattern:knowledge tree]
semantic_search map[query:project knowledge tree files storage location]
read_file map[end_line:130 path:docs/guide/memory.md start_line:60]
search_code map[file_type:go path:internal/know pattern:knowledge\.json|\.json"]
list_directory map[path:internal/know]
search_code map[file_type:go path:internal/know pattern:os\.(Create|OpenFile|WriteFile)|filepath\.Join]
search_code map[file_type:go path:internal/know pattern:"stamps|"cycles|stamps\.]
search_code map[file_type:go path:internal pattern:know/.*hash|knowDir|know/]
search_code map[file_type:go path:internal/know/stale.go pattern:stamps]
search_code map[file_type:go path:internal/know pattern:\.stamps|hashes|CheckStamps|loadStamps]
read_file map[end_line:60 path:internal/know/stale.go start_line:1]
read_file map[end_line:80 path:internal/think/know.go start_line:1]
search_code map[file_type:go path:internal/think pattern:stamps|\.json]

## tree after

