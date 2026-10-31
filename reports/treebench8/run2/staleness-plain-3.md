Where does armin keep the stamps of the files the tree cites, and what is in a stamp?

right true, 131072 prompt (113936 cached), 1476 completion, $0.00473, 10 rounds, 12 tools

## answer


**Where:** Beside the knowledge tree, in a file named `stamps` inside the tree's directory — `filepath.Join(g.Dir(), "stamps")` (internal/think/stale.go:56). The tree itself lives at `~/.local/share/armin/armin/know/<project key>/` (or under `$XDG_DATA_HOME`), so the stamps file sits right next to it.

**What's in a stamp:** the `stamp` struct (internal/think/stale.type.go:9-12) — what the cited file looked like at the last check:

- `Size`, `Mtime`, `Ctime` (int64)
- `Ino` — the inode (uint64)

The change time is included deliberately: unlike the modification time, it can't be set by `touch -d`, `cp -p`, `rsync -a`, so a replaced file is noticed even when size and mtime match.

**On-disk format** (stale.go:8-10): a uvarint count, then per file a uvarint-prefixed relative path and the four little-endian 64-bit values. At session start `refreshKnow` stats each cited file, re-hashes only the ones whose stamp changed (or that are gone), and rewrites the file atomically (temp + rename), sorted by path. A missing or damaged stamps file just means re-hashing everything once.

## calls
search_code map[pattern:stamp]
search_code map[file_type:go pattern:stamps]
search_code map[file_type:go pattern:stamp]
search_code map[file_type:go path:internal/think pattern:type stamp struct]
search_code map[file_type:go path:internal/know pattern:func \(g \*Graph\) Dir]
read_file map[end_line:12 path:internal/think/stale.type.go start_line:1]
search_code map[file_type:go path:internal/know pattern:knowledge]
search_code map[file_type:go path:internal/know pattern:knowledge|openPath|openTree]
search_code map[file_type:go path:internal/think pattern:know.Open|knowledgeTree|\.tree]
read_file map[end_line:40 path:internal/think/know.go start_line:1]
search_code map[file_type:go path:internal/session pattern:func ProjectDataDir]
search_code map[file_type:go path:internal/session pattern:func DataDir|UserConfigDir|os.User]

## tree after

