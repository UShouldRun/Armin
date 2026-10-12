package share

import (
  "crypto/sha256"
  "encoding/hex"
  "fmt"
  "net"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "time"

  "armin/internal/session"
)

// dialTimeout: a live host answers a local socket at once.
const dialTimeout = 200 * time.Millisecond

// Socket is where the host for root listens.
func Socket(root string) (string, error) {
  abs, err := filepath.Abs(root)
  if err != nil {
    return "", fmt.Errorf("\n\t... in Socket:\n\t%w", err)
  }

  dir, err := session.DataDir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in Socket:\n\t%w", err)
  }

  sum := sha256.Sum256([]byte(abs))

  return filepath.Join(dir, "attach", hex.EncodeToString(sum[:8]) + ".sock"), nil
}

// live reports whether a host answers at path.
func live(path string) bool {
  c, err := net.DialTimeout("unix", path, dialTimeout)
  if err != nil {
    return false
  }

  c.Close()

  return true
}

// pickInstance is the smallest number no live host has, so the
// numbers stay small and a restarted host gets its old one back.
func pickInstance(dir string) int {
  used := map[int]bool{}
  names, _ := filepath.Glob(filepath.Join(dir, "*.instance"))
  for _, name := range names {
    sock := strings.TrimSuffix(name, ".instance") + ".sock"
    if !live(sock) {
      continue
    }

    data, err := os.ReadFile(name)
    if err != nil {
      continue
    }

    n, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err == nil {
      used[n] = true
    }
  }

  n := 1
  for used[n] {
    n++
  }

  return n
}
