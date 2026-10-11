//go:build !linux

package llamacpp

import "os/exec"

// dieWithParent has no portable equivalent off Linux; there, cleanup
// relies on Shutdown and the remote dead man's switch alone.
func dieWithParent(cmd *exec.Cmd) {}
