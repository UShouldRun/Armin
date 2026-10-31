Where does armin keep the stamps of the files the tree cites, and what is in a stamp?

right true, 29129 prompt (17408 cached), 648 completion, $0.00244, 3 rounds, 3 tools

## answer







Armin keeps the stamps in a `stamps` file **beside the tree**, at `filepath.Join(g.Dir(), "stamps")` — internal/think/stale.go:45. `refreshKnow` reads the previous check's stamps with `readStamps`, compares each cited file's current stamp to it, and only reads and re-hashes files whose stamp changed or that are gone; after the check it rewrites the file atomically (tmp file + rename, mode 0600, sorted by path) via `writeStamps` (internal/think/stale.go:175-201).

**What a stamp is** (internal/think/stale.type.go:9-11) — four fields from `unix.Stat`:

- `Size int64` — file size
- `Mtime int64` — modification time, nanoseconds
- `Ctime int64` — change time, nanoseconds
- `Ino uint64` — inode number

ctime and inode are there because a *replaced* file (touch -d, cp -p, rsync -a) can match size+mtime, so a fresh inode/ctime catches it.

**On-disk encoding** (stale.go:141-170): a uvarint count, then per file a uvarint-prefixed relative path followed by the four little-endian 64-bit values. A missing or corrupt stamps file just means everything gets re-hashed once; only *cited* files (the `path:12-40` evidence refs the tree references) get stamped, and unchanged ones are counted as skipped (`stale.files.skipped`), so steady-state cost is O(cited file stats), not a re-read of anything.

## calls
search_code map[pattern:stamp]
read_file map[path:internal/think/stale.go]
search_code map[path:internal pattern:type stamp struct]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475912519947413"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-does-the-knowledge file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/think/stale.go:38 internal/know/stale.go:28 stale.go:156 stale.go:171 stale.go:188 internal/know/stale.go:71 hash.go:61 stale.go:118"
  evidence session "internal/know/hash.go" 00000000
  evidence session "internal/know/stale.go" 00000000
  evidence session "internal/think/stale.go" 00000000

