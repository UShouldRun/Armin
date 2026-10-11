package llamacpp

import (
  "os/exec"
  "syscall"
)

// dieWithParent asks the kernel to SIGTERM the child if armin dies
// without cleaning up (a crash, kill -9). For an SSH-run remote
// model that drops the session, which fires the dead man's switch.
func dieWithParent(cmd *exec.Cmd) {
  cmd.SysProcAttr = &syscall.SysProcAttr{ Pdeathsig: syscall.SIGTERM }
}
