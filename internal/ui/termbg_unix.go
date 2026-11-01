//go:build unix

// Asking the terminal its background color (OSC 11), so the logo can
// be drawn for it.

package ui

import (
  "os"
  "time"

  "golang.org/x/sys/unix"
)

// bgWait bounds the wait for the terminal's replies.
const bgWait = 300 * time.Millisecond

// termBackground asks the terminal its background and reports whether
// it's light, and whether it answered. The color query is followed by
// a device-attributes request, which every terminal answers, so the
// read stops at that answer and leaves nothing behind for the prompt.
func termBackground() (bool, bool) {
  if !isTTY(os.Stdin) || !isTTY(os.Stdout) {
    return false, false
  }

  fd := int(os.Stdin.Fd())
  state, err := enableCbreak(fd)
  if err != nil {
    return false, false
  }

  defer restoreMode(fd, state)

  os.Stdout.WriteString("\x1b]11;?\x1b\\\x1b[c")

  var reply []byte
  buf := make([]byte, 256)
  deadline := time.Now().Add(bgWait)
  for !daReply.Match(reply) {
    left := time.Until(deadline)
    if left <= 0 {
      break
    }

    fds := []unix.PollFd{ { Fd: int32(fd), Events: unix.POLLIN } }
    n, err := unix.Poll(fds, int(left / time.Millisecond) + 1)
    if err == unix.EINTR {
      continue
    }

    if err != nil || n == 0 {
      break
    }

    k, err := unix.Read(fd, buf)
    if err != nil || k <= 0 {
      break
    }

    reply = append(reply, buf[:k]...)
  }

  return parseBackground(reply)
}
