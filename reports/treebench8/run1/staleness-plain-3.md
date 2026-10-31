Where does armin keep the stamps of the files the tree cites, and what is in a stamp?

right true, 155759 prompt (139536 cached), 1642 completion, $0.00538, 11 rounds, 15 tools

## answer


Where: the knowledge tree lives under the per-project data dir, and the stamps are a plain file named `stamps` sitting right next to it —

```
<data>/armin/know/<project key>/stamps
```

- `<data>` is `XDG_DATA_HOME` or `~/.local/share` (session/session.go:48)
- `<project key>` is `session.ProjectDataDir("know", root)` (think/know.go:20-22)
- the path is built as `filepath.Join(g.Dir(), "stamps")` where `g.Dir()` is the tree's own directory (think/stale.go:56, know/store.go:189) — so it sits beside the tree's `graph.snap`/`graph.log`.

What a stamp is: a `stamp` struct — the `stat` of a cited file at the last check (stale.type.go:9-12):

```go
Size, Mtime, Ctime int64
Ino                uint64
```

- `Size` — file size
- `Mtime` — modification time (nanos)
- `Ctime` — change time (nanos) — included because mtime can be spoofed by tools (`touch -d`, `cp -p`, `rsync -a`), so a replaced file is caught even when size+mtime match
- `Ino` — inode

Each tree node that cites a file with `path:12-40` evidence points at that file, so `g.CitedFiles()` gives the set, and `refreshKnow` stats each, diffs against the `stamps` file, and only re-reads/re-hashes the ones whose stamp changed (or disappeared). Only the stamped fields that differ trigger a re-hash; the file is uvarint-encoded and written atomically (tmp + rename). A missing/corrupt stamps file just reads as empty, so everything gets re-hashed once.

Sources:
- `internal/think/stale.type.go:9-12` — the `stamp` struct
- `internal/think/stale.go:56-110` — building `stampsPath`, `stampOf`, `readStamps`, `writeStamps`
- `internal/think/stale.go:137-151` — `stampOf` filling Size/Mtime/Ctime/Ino from `unix.Stat`
- `internal/think/know.go:19-31` — `openKnow` → `session.ProjectDataDir("know", root)` → `know.Open(dir)`
- `internal/know/store.go:188-191` — `Graph.Dir()`
- `internal/session/session.go:48` — `DataDir` honoring `XDG_DATA_HOME`
- `docs/design/restructure-plan.md:136-137` — design note "stamps: size, inode, mtime and ctime of each cited file at the last staleness check"

## calls
search_code map[pattern:stamp]
project_manifest map[]
search_code map[path:internal/know/stale.go pattern:stamp]
find_files map[pattern:stale*.go]
search_code map[pattern:stampOf|readStamps|writeStamps]
read_file map[end_line:152 path:internal/think/stale.go start_line:120]
find_definition map[from_file:internal/think/stale.go name:func (g *Graph) Dir]
search_code map[path:internal/think/stale.go pattern:type stamp|Ino|Mtime]
search_code map[path:internal/know pattern:func .*Dir\(\)]
search_code map[pattern:know.*armin|Open.*know|LoadGraph|know\.]
search_code map[file_type:go path:internal pattern:know/(Open|Load)|OpenTree|LoadTree|knowledge]
read_file map[path:internal/think/know.go]
search_code map[pattern:type stamp struct]
search_code map[path:internal/session pattern:func ProjectDataDir|func DataDir]
read_file map[path:internal/think/stale.type.go]

## tree after

