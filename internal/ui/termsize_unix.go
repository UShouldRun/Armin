//go:build unix

// The terminal's width, from the TIOCGWINSZ ioctl.

package ui

import (
  "os"

  "golang.org/x/sys/unix"
)

// termCols is the terminal's width in columns, 0 when stdout isn't a
// terminal.
func termCols() int {
  ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
  if err != nil {
    return 0
  }

  return int(ws.Col)
}

// termRows is the terminal's height in rows, 0 when stdout isn't a
// terminal.
func termRows() int {
  ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
  if err != nil {
    return 0
  }

  return int(ws.Row)
}
