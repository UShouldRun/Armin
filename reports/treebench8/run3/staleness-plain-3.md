Where does armin keep the stamps of the files the tree cites, and what is in a stamp?

right true, 50806 prompt (43840 cached), 816 completion, $0.00217, 5 rounds, 6 tools

## answer


**Where:** a `stamps` file sitting beside the know tree — `internal/think/stale.go:56` builds the path as `filepath.Join(g.Dir(), "stamps")`, i.e. inside the tree's own directory (`g.Dir()` from `internal/know/store.go:189`). It's refreshed at the start of each session by `refreshKnow`: old stamps are read, current ones taken for every cited file, and the new map written back atomically (tmp file + rename).

**What's in a stamp:** the file's state at the last staleness check — `internal/think/stale.type.go:10-13`:

```go
type stamp struct {
  Size, Mtime, Ctime int64
  Ino                uint64
}
```

taken from `unix.Stat`. ctime and inode are deliberately included because a *replaced* file (via `touch -d`, `cp -p`, `rsync -a`) can match size+mtime but not change-time/inode.

**On-disk format** (`readStamps`/`writeStamps`, `stale.go:155-216`): binary — a uvarint count, then per entry a uvarint path-length + the path, followed by the four fields as little-endian uint64s, entries sorted by path. A missing or damaged file reads as an empty map, which just forces one full re-hash.

The purpose is staleness without any VCS: files whose stamp matches the last check are skipped entirely, so the steady-state pass is O(cited-file stats) rather than re-reading every file the tree cites.

## calls
search_code map[pattern:stamp]
search_code map[pattern:TreeStamp|Stamp]
read_file map[path:internal/think/stale.type.go]
read_file map[end_line:230 path:internal/think/stale.go start_line:150]
find_definition map[from_file:internal/think/stale.go name:Dir]
search_code map[path:internal pattern:func.*\bDir\(\)]

## tree after

