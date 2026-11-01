//go:build !unix

// Asking the terminal its background elsewhere: not done, so the logo
// falls back to the pixel art.

package ui

// termBackground doesn't ask where there's no raw mode to read the
// answer with.
func termBackground() (bool, bool) {
  return false, false
}
