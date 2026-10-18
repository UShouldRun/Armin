//go:build unix

package sandbox

import (
  "context"
  "os"
  "path/filepath"
  "strconv"
  "strings"
  "syscall"
  "testing"
  "time"
)

func TestRunTimeoutKillsProcessGroup(t *testing.T) {
  dir := t.TempDir()
  pidFile := filepath.Join(dir, "pid")

  c := &Config{ Root: dir, BuildTimeout: 300 * time.Millisecond }
  _, err := c.Exec(context.Background(), Command{ Binary: "sh", Args: []string{
    "-c", "sleep 30 & echo $! > " + pidFile + "; wait",
  }, Mode: ExecForce | ExecFull | ExecBuild })
  if err == nil || !strings.Contains(err.Error(), "timed out") {
    t.Fatalf("err = %v, want a timeout", err)
  }

  raw, err := os.ReadFile(pidFile)
  if err != nil {
    t.Fatal(err)
  }

  pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
  if err != nil {
    t.Fatal(err)
  }

  time.Sleep(100 * time.Millisecond)
  if syscall.Kill(pid, 0) == nil {
    syscall.Kill(pid, syscall.SIGKILL)
    t.Fatalf("grandchild %d outlived the timeout", pid)
  }
}
