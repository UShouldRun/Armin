//go:build darwin

// The termios ioctls on macOS.

package ui

import "golang.org/x/sys/unix"

const (
  ioctlGetTermios = unix.TIOCGETA
  ioctlSetTermios = unix.TIOCSETA
)
