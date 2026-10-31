Where does armin keep the stamps of the files the tree cites, and what is in a stamp?

right true, 35096 prompt (30736 cached), 829 completion, $0.00185, 4 rounds, 5 tools

## answer


The stamps live in a file named **`stamps`, next to the knowledge tree** in `g.Dir()` — not in the tree file itself (`internal/think/stale.go:56`).

**What a stamp is** — one per cited file, in the `stamp` struct (`internal/think/stalk/stale.type.go:9`, i.e. `internal/think/stale.type.go:9`):

- `Size int64` — file size
- `Mtime int64` — modification time (nanoseconds)
- `Ctime int64` — change time (nanoseconds)
- `Ino uint64` — inode number

`stampOf` (`stale.go:138`) fills it from `unix.Stat` (Linux `statx` fields). The extra fields beyond size+mtime are deliberate: `Ino` catches a file that was *replaced* with one of identical size and mtime, and `Ctime` catches timestamp-preserving copies (`cp -p`, `rsync -a`) — the doc comment at `stale.type.go:7` spells this out.

**File format** (documented in the package comment, `stale.go:8-10`): a uvarint count, then per file a uvarint length-prefixed path (capped at 4096 bytes), then the four values as little-endian 64-bit ints. `readStamps` (`stale.go:155`) treats a missing or damaged file as an empty map, so the whole set gets re-hashed once; `writeStamps` (`stale.go:188`) writes entries sorted by path through a temp file + rename, so it's atomic.

So the flow per session start (`refreshKnow`, `stale.go:30`): stat every file the tree cites → compare against the `stamps` file → only files whose stamp changed (or disappeared) get re-read and re-hashed by `know.CheckFile`; then the new stamps are written back.

## calls
search_code map[pattern:stamp]
find_files map[pattern:*stamp*]
read_file map[end_line:55 path:internal/think/stale.go start_line:11]
read_file map[end_line:200 path:internal/think/stale.go start_line:130]
search_code map[path:internal/think pattern:type stamp struct]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031593827526"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/know/hash.go:14-46 internal/think/stale.go:37 internal/know/stale.go:24 stale.go:145 internal/know/stale.go:59 stale.go:88 hash.go:57 stale.go:101 stale.go:120"
  evidence session "internal/know/doc.go" 00000000
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/line.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

