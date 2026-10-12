//go:build !unix

package ui

import (
  "os"
  "strconv"
)

// termCols falls back to $COLUMNS where there's no ioctl.
func termCols() int {
  n, _ := strconv.Atoi(os.Getenv("COLUMNS"))
  return n
}

// termRows falls back to $LINES where there's no ioctl.
func termRows() int {
  n, _ := strconv.Atoi(os.Getenv("LINES"))
  return n
}
