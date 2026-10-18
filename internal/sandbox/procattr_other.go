//go:build !unix

package sandbox

import "os/exec"

// detachTerminal has no equivalent off Unix; there, noPrompt
// alone keeps git from prompting.
func detachTerminal(cmd *exec.Cmd) {}
