// Package session persists conversations to disk so they can be
// resumed: each session is one file named by its id (a UUID), whose
// first line carries the project directory, model, a title the model
// wrote, and timestamps.
//
// The on-disk format is JSON Lines, not one big JSON document: a
// metadata line followed by one line per message. That's what makes
// Append possible — writing only the messages added since the last
// save, instead of re-serializing the entire, ever-growing history on
// every turn. A REPL that autosaves after each turn and rewrote the
// whole file every time would do O(n) work per turn and O(n²) total
// disk I/O over an n-turn session; Append does O(1) amortized work
// per turn and O(n) total.
//
// Sessions saved before ids existed (one file per project, named
// <dir>-<hash>.jsonl) still list and load: their id is that name.
package session

import (
  "bufio"
  "crypto/rand"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "sort"
  "strings"
  "time"

  "armin/internal/llm"
)

// NewID returns a random RFC 4122 v4 UUID.
func NewID() string {
  var b [16]byte
  rand.Read(b[:])
  b[6] = b[6] & 0x0f | 0x40
  b[8] = b[8] & 0x3f | 0x80

  return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// DataDir returns armin's data directory, honoring XDG_DATA_HOME.
func DataDir() (string, error) {
  base := os.Getenv("XDG_DATA_HOME")
  if base == "" {
    home, err := os.UserHomeDir()
    if err != nil {
      return "", fmt.Errorf("\n\t... in DataDir:\n\t%w", err)
    }

    base = filepath.Join(home, ".local", "share")
  }

  return filepath.Join(base, "armin"), nil
}

// Dir returns the session storage directory.
func Dir() (string, error) {
  base, err := DataDir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in Dir:\n\t%w", err)
  }

  return filepath.Join(base, "sessions"), nil
}

// ProjectKey names a project root on disk: its base name plus a
// short hash of its absolute path, so two "api" checkouts don't
// collide.
func ProjectKey(root string) (string, error) {
  abs, err := filepath.Abs(root)
  if err != nil {
    return "", fmt.Errorf("\n\t... in ProjectKey:\n\t%w", err)
  }

  sum := sha256.Sum256([]byte(abs))

  return filepath.Base(abs) + "-" + hex.EncodeToString(sum[:])[:8], nil
}

// ProjectDataDir is <data>/armin/<kind>/<project key> — where
// per-project state other than sessions (memory, scratch) lives.
func ProjectDataDir(kind, root string) (string, error) {
  base, err := DataDir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in ProjectDataDir:\n\t%w", err)
  }

  key, err := ProjectKey(root)
  if err != nil {
    return "", fmt.Errorf("\n\t... in ProjectDataDir:\n\t%w", err)
  }

  return filepath.Join(base, kind, key), nil
}

func pathFor(id string) (string, error) {
  if id == "" || strings.ContainsAny(id, `/\`) || strings.HasPrefix(id, ".") {
    return "", fmt.Errorf("\n\t... in pathFor:\n\t invalid session id %q", id)
  }

  dir, err := Dir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in pathFor:\n\t%w", err)
  }

  return filepath.Join(dir, id + ".jsonl"), nil
}

// Save writes a fresh snapshot of the session, replacing whatever
// was there: for infrequent, whole-history writes (a rename, /save,
// the exit flush). The per-turn path is Append.
func Save(m *Meta, history []llm.Message) error {
  path, err := pathFor(m.ID)
  if err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  err = os.MkdirAll(filepath.Dir(path), 0o755)
  if err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  f, err := os.Create(path)
  if err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  defer f.Close()

  err = writeAll(f, m, history)
  if err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  return nil
}

func writeAll(w io.Writer, m *Meta, history []llm.Message) error {
  now := time.Now()
  if m.CreatedAt.IsZero() {
    m.CreatedAt = now
  }

  m.UpdatedAt = now

  enc := json.NewEncoder(w)
  err := enc.Encode(m)
  if err != nil {
    return err
  }

  for _, msg := range history {
    err := enc.Encode(msg)
    if err != nil {
      return err
    }
  }

  return nil
}

// Append writes only history[*persisted:] — the messages added
// since the last Save or Append — then advances *persisted to
// len(history). If *persisted is 0 (nothing written yet this run)
// it falls back to a full Save, which also lays down the metadata
// line.
//
// *persisted must accurately reflect what's already on disk;
// callers that mutate history in place (/clear, /compact) rather
// than only appending should Save and reset *persisted themselves.
func Append(m *Meta, history []llm.Message, persisted *int) error {
  if *persisted == 0 {
    err := Save(m, history)
    if err != nil {
      return err
    }

    *persisted = len(history)

    return nil
  }

  if *persisted >= len(history) {
    return nil // nothing new
  }

  path, err := pathFor(m.ID)
  if err != nil {
    return fmt.Errorf("\n\t... in Append:\n\t%w", err)
  }

  f, err := os.OpenFile(path, os.O_APPEND | os.O_WRONLY, 0o644)
  if err != nil {
    return fmt.Errorf("\n\t... in Append:\n\t%w", err)
  }

  defer f.Close()

  enc := json.NewEncoder(f)
  for _, msg := range history[*persisted:] {
    err := enc.Encode(msg)
    if err != nil {
      return fmt.Errorf("\n\t... in Append:\n\t%w", err)
    }
  }

  *persisted = len(history)

  return nil
}

// Load reads a session by id, or by a prefix that matches exactly
// one session.
func Load(id string) (*Saved, error) {
  path, err := resolve(id)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  f, err := os.Open(path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  defer f.Close()

  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 0, 64 * 1024), 8 * 1024 * 1024)

  if !scanner.Scan() {
    return nil, fmt.Errorf("\n\t... in Load:\n\t %s: empty session file", path)
  }

  m, err := parseMeta(scanner.Bytes(), path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t parsing metadata: %w", err)
  }

  var history []llm.Message
  for scanner.Scan() {
    var msg llm.Message
    err := json.Unmarshal(scanner.Bytes(), &msg)
    if err != nil {
      return nil, fmt.Errorf(
        "\n\t... in Load:\n\t parsing message %d: %w", len(history), err,
      )
    }

    history = append(history, msg)
  }

  err = scanner.Err()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  m.Messages = len(history)

  return &Saved{ Meta: m, History: history }, nil
}

// resolve finds the file for an id or a unique id prefix.
func resolve(id string) (string, error) {
  path, err := pathFor(id)
  if err == nil {
    _, err := os.Stat(path)
    if err == nil {
      return path, nil
    }
  }

  list, err := List()
  if err != nil {
    return "", err
  }

  var hits []Meta
  for _, m := range list {
    if strings.HasPrefix(m.ID, id) {
      hits = append(hits, m)
    }
  }

  switch len(hits) {
  case 0:
    return "", fmt.Errorf("no session with id %q (armin -resume lists them)", id)
  case 1:
    return pathFor(hits[0].ID)
  }

  return "", fmt.Errorf("%q matches %d sessions; use more of the id", id, len(hits))
}

// parseMeta reads a first line, filling what a legacy file lacks:
// its id is its file name.
func parseMeta(line []byte, path string) (Meta, error) {
  var m Meta
  err := json.Unmarshal(line, &m)
  if err != nil {
    return Meta{}, err
  }

  if m.ID == "" {
    m.ID = strings.TrimSuffix(filepath.Base(path), ".jsonl")
  }

  if m.CreatedAt.IsZero() {
    m.CreatedAt = m.UpdatedAt
  }

  return m, nil
}

// List returns every saved session, most recently updated first. It
// reads each file's first line and counts the rest, never parsing
// the messages themselves.
func List() ([]Meta, error) {
  dir, err := Dir()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in List:\n\t%w", err)
  }

  entries, err := os.ReadDir(dir)
  if os.IsNotExist(err) {
    return nil, nil
  }

  if err != nil {
    return nil, fmt.Errorf("\n\t... in List:\n\t%w", err)
  }

  var out []Meta
  for _, e := range entries {
    if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
      continue
    }

    m, ok := readMeta(filepath.Join(dir, e.Name()))
    if ok {
      out = append(out, m)
    }
  }

  sort.Slice(out, func(i, j int) bool {
    return out[i].UpdatedAt.After(out[j].UpdatedAt)
  })

  return out, nil
}

func readMeta(path string) (Meta, bool) {
  f, err := os.Open(path)
  if err != nil {
    return Meta{}, false
  }

  defer f.Close()

  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 0, 64 * 1024), 8 * 1024 * 1024)
  if !scanner.Scan() {
    return Meta{}, false
  }

  m, err := parseMeta(scanner.Bytes(), path)
  if err != nil {
    return Meta{}, false
  }

  for scanner.Scan() {
    m.Messages++
  }

  return m, true
}

// Latest is the most recently updated session for root, or nil.
func Latest(root string) (*Saved, error) {
  abs, err := filepath.Abs(root)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Latest:\n\t%w", err)
  }

  list, err := List()
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Latest:\n\t%w", err)
  }

  for _, m := range list {
    if m.Root == abs {
      return Load(m.ID)
    }
  }

  return nil, nil
}

// Delete removes a session file (by id or unique prefix) and returns
// what it was, so the caller can clean up what hangs off it (its
// scratchpad).
func Delete(id string) (Meta, error) {
  path, err := resolve(id)
  if err != nil {
    return Meta{}, fmt.Errorf("\n\t... in Delete:\n\t%w", err)
  }

  m, ok := readMeta(path)
  if !ok {
    m = Meta{ ID: strings.TrimSuffix(filepath.Base(path), ".jsonl") }
  }

  err = os.Remove(path)
  if err != nil {
    return Meta{}, fmt.Errorf("\n\t... in Delete:\n\t%w", err)
  }

  return m, nil
}
