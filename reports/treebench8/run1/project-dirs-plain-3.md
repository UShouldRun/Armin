Where are a project's knowledge tree files stored, and what are they called?

right true, 137958 prompt (120640 cached), 1293 completion, $0.01113, 9 rounds, 11 tools

## answer





















A project's knowledge tree lives under `~/.local/share/armin/know/<project>-<hash>/` — the dir is built by `session.ProjectDataDir("know", root)` (internal/think/know.go:21-25; the `<project key>` is the project name plus a hash, per docs/guide/memory.md:76-78). It's a directory, not a single file, with these files (per docs/design/restructure-plan.md:128-141 and internal/know/store.go, snap.go):

| File | What it is |
|---|---|
| `graph.snap` | The tree itself: binary snapshot, magic `"ARMK"`, holding the node, edge, evidence and author arrays (written atomically on compaction, snap.go:226) |
| `graph.log` | Append-only op log, `"ARML"` + generation, then `[op u8][len uvarint][payload][crc32]` records; replayed over the snapshot on load (store.go) |
| `lock` | flock'd by the armin that writes; other armins open the tree read-only (store.go:76-80) |
| `stamps` | size/inode/mtime/ctime of each cited file at the last staleness check, used by the `refreshKnow` staleness pass (internal/know/stale.go) |
| `events` | optional, learner trees only: append-only raw 12-byte `Event` records |

Internally it's implemented by the `know` package (`internal/know`, type `know.Graph`). You can view or edit it with `armin -know dump` / `armin -know edit`, which produce a human-readable text form (internal/know/dump.go) — one block per node with a path like `tcp/handshake`, claim, confidence, evidence and author.

## calls
search_code map[pattern:knowledge tree]
search_code map[pattern:knowledge.?tree]
read_file map[end_line:120 path:/home/henrique/Code/armin/docs/guide/memory.md start_line:70]
list_directory map[path:internal/know]
search_code map[path:internal/know pattern:"nodes|\.json|filename|File\(]
search_code map[path:internal/know pattern:graph.log|graph.snap|stamps|lock]
search_code map[file_type:go path:internal pattern:know/|\.local/share/armin|"know"]
search_code map[path:internal/know pattern:"stamps]
search_code map[pattern:stamps]
read_file map[end_line:145 path:/home/henrique/Code/armin/docs/design/restructure-plan.md start_line:110]
find_definition map[name:ProjectDataDir]

## tree after

