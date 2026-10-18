//go:build linux

// The termios ioctls on Linux.

package ui

import "golang.org/x/sys/unix"

const (
  ioctlGetTermios = unix.TCGETS
  ioctlSetTermios = unix.TCSETS
)
