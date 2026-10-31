// The tree's staleness check at session start, with no version
// control needed: each cited file's stamp (size, inode, modification
// and change time) is compared with the last check's, and only files
// whose stamp changed, or that are gone, are re-hashed by know's
// CheckFile; then MarkSuspect and FlagCycles. Git, Mercurial, Jujutsu,
// Subversion, Fossil or none: the same O(cited files) stats.
//
// The stamps live beside the tree, in "stamps": a uvarint count, then
// per file a uvarint-prefixed path and four little-endian 64-bit
// values.

package think

import (
  "bufio"
  "bytes"
  "encoding/binary"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "golang.org/x/sys/unix"

  "armin/internal/know"
)

// refreshKnow brings the tree up to date with the code and says what
// it found, or "" when nothing changed.
func refreshKnow(root string, g *know.Graph) (string, error) {
  if g.ReadOnly() {
    return "", nil
  }

  cited := map[string][]know.EvidenceID{}
  for path, evs := range g.CitedFiles() {
    rel := path
    if filepath.IsAbs(path) {
      r, err := filepath.Rel(root, path)
      if err != nil || strings.HasPrefix(r, "..") {
        continue
      }

      rel = r
    }

    cited[rel] = append(cited[rel], evs...)
  }

  stampsPath := filepath.Join(g.Dir(), "stamps")
  old := readStamps(stampsPath)
  now := make(map[string]stamp, len(cited))
  var stale []know.NodeID
  moved := 0
  for rel, evs := range cited {
    st, ok := stampOf(filepath.Join(root, rel))
    if ok {
      now[rel] = st
    }

    prev, seen := old[rel]
    if ok && seen && prev == st {
      continue
    }

    data, err := os.ReadFile(filepath.Join(root, rel))
    gone := os.IsNotExist(err)
    if err != nil && !gone {
      continue
    }

    s, m, err := g.CheckFile(rel, data, gone, evs)
    if err != nil {
      return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
    }

    stale = append(stale, s...)
    moved += m
  }

  suspect, err := g.MarkSuspect(stale)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  author, err := g.Intern(know.RoleArmin, "", "", 0)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  loops, err := g.FlagCycles(author)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  err = writeStamps(stampsPath, now)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  var parts []string
  for _, p := range []struct {
    n    int
    what string
  }{
    { len(stale), "stale" }, { suspect, "suspect" },
    {
      moved, "references followed moved code",
    }, { len(loops), "open requires loops" },
  } {
    if p.n > 0 {
      parts = append(parts, fmt.Sprintf("%d %s", p.n, p.what))
    }
  }

  if len(parts) == 0 {
    return "", nil
  }

  return "knowledge tree: " + strings.Join(parts, ", "), nil
}

// stampOf stats path; false when it can't (gone, unreadable).
func stampOf(path string) (stamp, bool) {
  var st unix.Stat_t
  err := unix.Stat(path, &st)
  if err != nil {
    return stamp{}, false
  }

  return stamp{
    Size:  st.Size,
    Mtime: st.Mtim.Nano(),
    Ctime: st.Ctim.Nano(),
    Ino:   uint64(st.Ino),
  }, true
}

// readStamps loads the last check's stamps; a missing or damaged file
// is an empty map, which re-hashes everything once.
func readStamps(path string) map[string]stamp {
  out := map[string]stamp{}
  data, err := os.ReadFile(path)
  if err != nil {
    return out
  }

  r := bufio.NewReader(bytes.NewReader(data))
  n, err := binary.ReadUvarint(r)
  if err != nil {
    return out
  }

  for i := uint64(0); i < n; i++ {
    size, err := binary.ReadUvarint(r)
    if err != nil || size > 4096 {
      return map[string]stamp{}
    }

    name := make([]byte, size)
    _, err = io.ReadFull(r, name)
    var st stamp
    if err == nil {
      err = binary.Read(r, binary.LittleEndian, &st)
    }

    if err != nil {
      return map[string]stamp{}
    }

    out[string(name)] = st
  }

  return out
}

// writeStamps saves stamps sorted by path, through a temporary file
// and a rename.
func writeStamps(path string, stamps map[string]stamp) error {
  names := make([]string, 0, len(stamps))
  for name := range stamps {
    names = append(names, name)
  }

  sort.Strings(names)
  var b bytes.Buffer
  b.Write(binary.AppendUvarint(nil, uint64(len(names))))
  for _, name := range names {
    b.Write(binary.AppendUvarint(nil, uint64(len(name))))
    b.WriteString(name)
    st := stamps[name]
    binary.Write(&b, binary.LittleEndian, &st)
  }

  tmp := path + ".tmp"
  err := os.WriteFile(tmp, b.Bytes(), 0o600)
  if err != nil {
    return err
  }

  return os.Rename(tmp, path)
}
