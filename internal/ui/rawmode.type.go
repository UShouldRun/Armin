//go:build unix

package ui

import (
  "golang.org/x/sys/unix"
)

// rawState is what restoreMode needs to put the terminal back
// exactly how it was.
type rawState struct {
  termios unix.Termios
}
