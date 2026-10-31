// The types of stale.go.

package think

// stamp is what a cited file looked like at the last check. The
// change time can't be set by tools the way the modification time can
// (touch -d, cp -p, rsync -a), so a replaced file is noticed even when
// its size and modification time match.
type stamp struct {
  Size, Mtime, Ctime int64
  Ino                uint64
}
