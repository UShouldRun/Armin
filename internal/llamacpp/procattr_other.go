//go:build !linux

// Elsewhere: no parent-death signal; Shutdown stops the servers.

package llamacpp

import "os/exec"

// dieWithParent has no portable equivalent off Linux; there, cleanup
// relies on Shutdown and the remote dead man's switch alone.
func dieWithParent(cmd *exec.Cmd) {}
