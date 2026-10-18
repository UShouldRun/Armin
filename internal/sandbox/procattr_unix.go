//go:build unix

package sandbox

import (
  "os/exec"
  "syscall"
)

// detachTerminal starts the child in its own session, with no
// controlling terminal: a program that would prompt on /dev/tty
// (ssh asking for a passphrase, git for a password) fails at once
// instead of hanging, unseen behind the REPL, until the timeout.
//
// The session is also a process group, and a timeout kills all of
// it: killing only the child leaves what it spawned (git pull's
// fetch and ssh) running, orphaned, after armin has moved on.
func detachTerminal(cmd *exec.Cmd) {
  cmd.SysProcAttr = &syscall.SysProcAttr{ Setsid: true }
  cmd.Cancel = func() error {
    return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
  }
}
