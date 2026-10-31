// Open and Close a tree directory: take the lock (or fall back to
// read-only when another armin holds it), mmap graph.snap, replay
// graph.log and cut off a torn tail; New is the same graph with no
// files, for tests and scratch trees.

package know

import (
  "encoding/binary"
  "errors"
  "fmt"
  "os"
  "path/filepath"

  "golang.org/x/sys/unix"
)

// New returns an in-memory tree holding only the root, with no files.
func New() *Graph {
  g := &Graph{ moreArena: []byte{ 0 } }
  err := g.root()
  if err != nil {
    panic(err) // only a bug in root can fail on an empty graph
  }

  return g
}

// root adds the user author and the root node to an empty graph.
func (g *Graph) root() error {
  _, err := g.Intern(RoleUser, "", "", 0)
  if err != nil {
    return err
  }

  d := today()
  g.begin(opNode)
  g.uv(0)
  g.uv(0)
  g.uv(uint64(kindConf(KindTopic, ConfChecked)))
  g.uv(0)
  g.uv(uint64(d))
  g.uv(uint64(d))
  g.uv(0)
  g.str("")
  g.str("")

  return g.commit()
}

// Open loads the tree stored in dir, creating it if needed. When
// another process holds the tree, it opens read-only and every
// mutation returns ErrReadOnly.
func Open(dir string) (*Graph, error) {
  if !littleEndian() {
    return nil, errors.New("\n\t... in Open:\n\tknow needs a little-endian machine")
  }

  err := os.MkdirAll(dir, 0o700)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Open:\n\t%w", err)
  }

  g := &Graph{ dir: dir }
  err = g.load()
  if err != nil {
    g.Close()
    return nil, fmt.Errorf("\n\t... in Open:\n\t%s: %w", dir, err)
  }

  return g, nil
}

func (g *Graph) load() error {
  if g.lock == nil {
    lock, err := os.OpenFile(
      filepath.Join(g.dir, "lock"), os.O_RDWR | os.O_CREATE, 0o600,
    )
    if err != nil {
      return err
    }

    g.lock = lock
    err = unix.Flock(int(lock.Fd()), unix.LOCK_EX | unix.LOCK_NB)
    if err != nil {
      g.ro = true
    }
  }

  err := g.loadSnap(filepath.Join(g.dir, "graph.snap"))
  if err != nil && !os.IsNotExist(err) {
    return err
  }

  if err != nil {
    g.moreArena = []byte{ 0 }
  }

  g.edges.reverse(g.Len())
  g.pathRebuild(g.Len())

  err = g.openLog()
  if err != nil {
    return err
  }

  if g.Len() == 0 {
    if g.ro {
      g.ro = false
      defer func() { g.ro = true }()
    }

    return g.root()
  }

  return nil
}

// openLog replays graph.log and, holding the lock, reopens it for
// appending. A log older than the snapshot is what a crash between
// compaction's two renames leaves, and is replaced.
func (g *Graph) openLog() error {
  path := filepath.Join(g.dir, "graph.log")
  f, err := os.Open(path)
  if err != nil && !os.IsNotExist(err) {
    return err
  }

  good := int64(0)
  if err == nil {
    var gen uint64
    var replayErr error
    good, gen, replayErr = g.replay(f)
    f.Close()
    if replayErr != nil {
      return replayErr
    }

    if gen > g.logGen {
      return fmt.Errorf(
        "graph.log generation %d is newer than the snapshot's %d", gen, g.logGen,
      )
    }
  }

  if g.ro {
    return nil
  }

  if good == 0 {
    return g.newLog()
  }

  err = os.Truncate(path, good)
  if err != nil {
    return err
  }

  g.log, err = os.OpenFile(path, os.O_WRONLY | os.O_APPEND, 0o600)

  return err
}

// newLog replaces graph.log with an empty one of the current
// generation.
func (g *Graph) newLog() error {
  if g.log != nil {
    g.log.Close()
    g.log = nil
  }

  path := filepath.Join(g.dir, "graph.log")
  head := append([]byte(logMagic), make([]byte, 8)...)
  binary.LittleEndian.PutUint64(head[4:], g.logGen)
  err := writeFile(path, head)
  if err != nil {
    return err
  }

  g.log, err = os.OpenFile(path, os.O_WRONLY | os.O_APPEND, 0o600)

  return err
}

// Dir is the tree's directory ("" for an in-memory tree).
func (g *Graph) Dir() string {
  return g.dir
}

// ReadOnly reports whether another process holds the tree.
func (g *Graph) ReadOnly() bool {
  return g.ro
}

// NeedsCompact reports whether the log has grown past half the
// snapshot (and past 64 KiB, so small trees don't churn).
func (g *Graph) NeedsCompact() bool {
  if g.log == nil {
    return false
  }

  st, err := g.log.Stat()
  if err != nil {
    return false
  }

  return st.Size() > 64 * 1024 && st.Size() > int64(len(g.mapped)) / 2
}

// Compact writes a fresh snapshot, starts an empty log and reloads
// from them. Evidence IDs change (deleted evidence is dropped and the
// rest sorted by node); node and author IDs never do.
func (g *Graph) Compact() error {
  if g.dir == "" {
    return nil
  }

  if g.ro {
    return ErrReadOnly
  }

  gen := g.logGen + 1
  err := writeFile(filepath.Join(g.dir, "graph.snap"), g.encodeSnap(gen))
  if err != nil {
    return fmt.Errorf("\n\t... in Compact:\n\t%w", err)
  }

  // keep holding the lock across the reload
  lock := g.lock
  g.lock = nil
  g.unload()
  g.lock = lock
  err = g.load()
  if err != nil {
    return fmt.Errorf("\n\t... in Compact:\n\t%w", err)
  }

  return nil
}

// Changed reports whether the log holds anything since the snapshot.
func (g *Graph) Changed() bool {
  if g.log == nil {
    return false
  }

  st, err := g.log.Stat()

  return err == nil && st.Size() > int64(len(logMagic) + 8)
}

// Sync flushes the log to disk.
func (g *Graph) Sync() error {
  if g.log == nil {
    return nil
  }

  return g.log.Sync()
}

// unload releases the files and the mapping and empties the graph.
func (g *Graph) unload() {
  if g.log != nil {
    g.log.Sync()
    g.log.Close()
  }

  if g.mapped != nil {
    unix.Munmap(g.mapped)
  }

  if g.lock != nil {
    g.lock.Close()
  }

  *g = Graph{ dir: g.dir }
}

// Close syncs and releases the tree; nothing read from it with Bytes
// may be used afterwards.
func (g *Graph) Close() error {
  var err error
  if g.log != nil {
    err = g.log.Sync()
  }

  g.unload()

  return err
}
