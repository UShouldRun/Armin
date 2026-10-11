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
