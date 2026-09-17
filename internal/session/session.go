// Package session persists conversation history to disk so
// --resume can pick a project back up without starting cold (plan
// Phase 4). One file per project root, keyed by a short hash of its
// absolute path.
//
// The on-disk format is JSON Lines, not one big JSON document: a
// metadata line followed by one line per message. That's what makes
// Append below possible — writing only the messages added since the
// last save, instead of re-serializing and rewriting the entire,
// ever-growing history on every single turn. A REPL that autosaves
// after each turn and rewrote the whole file every time would do
// O(n) work per turn and O(n^2) total disk I/O over an n-turn
// session; Append does O(1) amortized work per turn and O(n) total.
package session

import (
  "bufio"
  "crypto/sha256"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "io"
  "os"
  "path/filepath"
  "time"

  "armin/internal/llm"
)

// Saved is what Load reconstructs from a session file.
type Saved struct {
  Root, Model string
  UpdatedAt   time.Time
  History     []llm.Message
}

// meta is the first line of the JSONL file.
type meta struct {
  Root      string    `json:"root"`
  Model     string    `json:"model"`
  UpdatedAt time.Time `json:"updated_at"`
}

// Dir returns the session storage directory, honoring
// XDG_DATA_HOME.
func Dir() (string, error) {
  base := os.Getenv("XDG_DATA_HOME")
  if base == "" {
    home, err := os.UserHomeDir()
    if err != nil {
      return "", fmt.Errorf("\n\t... in Dir:\n\t%w", err)
    }
    base = filepath.Join(home, ".local", "share")
  }
  return filepath.Join(base, "armin", "sessions"), nil
}

func pathFor(root string) (string, error) {
  dir, err := Dir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in pathFor:\n\t%w", err)
  }

  abs, err := filepath.Abs(root)
  if err != nil {
    return "", fmt.Errorf("\n\t... in pathFor:\n\t%w", err)
  }

  sum := sha256.Sum256([]byte(abs))
  hash := hex.EncodeToString(sum[:])[:8]
  name := filepath.Base(abs) + "-" + hash + ".jsonl"
  return filepath.Join(dir, name), nil
}

// Save writes a fresh snapshot of history to disk, replacing
// whatever was there. Use this for infrequent, whole-history writes
// (an explicit /save, or the final flush on exit) — the per-turn
// autosave path should use Append instead.
func Save(root, model string, history []llm.Message) error {
  path, err := pathFor(root)
  if err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }
  if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  f, err := os.Create(path)
  if err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }
  defer f.Close()

  if err := writeAll(f, root, model, history); err != nil {
    return fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }
  return nil
}

func writeAll(
  w io.Writer, root, model string, history []llm.Message,
) error {
  enc := json.NewEncoder(w)
  head := meta{ Root: root, Model: model, UpdatedAt: time.Now() }
  if err := enc.Encode(head); err != nil {
    return err
  }
  for _, m := range history {
    if err := enc.Encode(m); err != nil {
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
// callers that mutate history in place (e.g. /clear, /compact)
// rather than only appending to it should call Save and reset
// *persisted themselves instead of calling Append, or the log ends
// up with lines that no longer correspond to the in-memory history.
func Append(
  root, model string, history []llm.Message, persisted *int,
) error {
  if *persisted == 0 {
    if err := Save(root, model, history); err != nil {
      return err
    }
    *persisted = len(history)
    return nil
  }
  if *persisted >= len(history) {
    return nil // nothing new
  }

  path, err := pathFor(root)
  if err != nil {
    return fmt.Errorf("\n\t... in Append:\n\t%w", err)
  }

  f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
  if err != nil {
    return fmt.Errorf("\n\t... in Append:\n\t%w", err)
  }
  defer f.Close()

  enc := json.NewEncoder(f)
  for _, m := range history[*persisted:] {
    if err := enc.Encode(m); err != nil {
      return fmt.Errorf("\n\t... in Append:\n\t%w", err)
    }
  }

  *persisted = len(history)
  return nil
}

// Load reads the saved session for root. A missing session returns
// (nil, nil) — it's a normal, expected state, not an error.
func Load(root string) (*Saved, error) {
  path, err := pathFor(root)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  f, err := os.Open(path)
  if os.IsNotExist(err) {
    return nil, nil
  }
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }
  defer f.Close()

  scanner := bufio.NewScanner(f)
  scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

  if !scanner.Scan() {
    return nil, fmt.Errorf(
      "\n\t... in Load:\n\t %s: empty session file", path,
    )
  }
  var m meta
  if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Load:\n\t parsing metadata: %w", err,
    )
  }

  var history []llm.Message
  for scanner.Scan() {
    var msg llm.Message
    if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
      return nil, fmt.Errorf(
        "\n\t... in Load:\n\t parsing message %d: %w",
        len(history), err,
      )
    }
    history = append(history, msg)
  }
  if err := scanner.Err(); err != nil {
    return nil, fmt.Errorf("\n\t... in Load:\n\t%w", err)
  }

  return &Saved{
    Root:      m.Root,
    Model:     m.Model,
    UpdatedAt: m.UpdatedAt,
    History:   history,
  }, nil
}
