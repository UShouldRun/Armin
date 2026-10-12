//go:build unix

package ui

import "golang.org/x/sys/unix"

// enableRawMode puts fd into raw mode — no canonical line editing,
// no echo, no signal generation from Ctrl-C/Ctrl-Z — and returns
// the previous state to restore later. This is the same
// transformation POSIX's cfmakeraw performs, hand-rolled against
// golang.org/x/sys/unix (already in the dependency tree via the
// standard library's own transitive needs) rather than adding
// golang.org/x/term for one function.
func enableRawMode(fd int) (*rawState, error) {
  termios, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
  if err != nil {
    return nil, err
  }
  prev := *termios

  raw := *termios
  raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
    unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
  raw.Oflag &^= unix.OPOST
  raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON |
    unix.ISIG | unix.IEXTEN
  raw.Cflag &^= unix.CSIZE | unix.PARENB
  raw.Cflag |= unix.CS8
  raw.Cc[unix.VMIN] = 1
  raw.Cc[unix.VTIME] = 0

  err = unix.IoctlSetTermios(fd, ioctlSetTermios, &raw)
  if err != nil {
    return nil, err
  }
  return &rawState{ termios: prev }, nil
}

// enableCbreak turns off line buffering and echo only: keys arrive
// one at a time and aren't printed, while output keeps its newline
// handling and Ctrl-C still interrupts — for typing beside a turn
// that's printing.
func enableCbreak(fd int) (*rawState, error) {
  termios, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
  if err != nil {
    return nil, err
  }

  prev := *termios
  cb := *termios
  cb.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON
  cb.Iflag &^= unix.ICRNL
  cb.Cc[unix.VMIN] = 1
  cb.Cc[unix.VTIME] = 0

  err = unix.IoctlSetTermios(fd, ioctlSetTermios, &cb)
  if err != nil {
    return nil, err
  }

  return &rawState{ termios: prev }, nil
}

func restoreMode(fd int, state *rawState) error {
  if state == nil {
    return nil
  }
  return unix.IoctlSetTermios(fd, ioctlSetTermios, &state.termios)
}
