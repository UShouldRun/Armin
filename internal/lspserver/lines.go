// Finding line n of a document without splitting the whole text,
// for hover and completion, which run on every keystroke.

package lspserver

// lineOffset returns the byte range [start, end) of line n
// (0-indexed, end excludes the trailing newline) in text, or
// ok=false if text has fewer than n+1 lines. This is the shared
// primitive contextWindow and splitAtPosition use instead of
// strings.Split(text, "\n"): Split allocates one string header per
// line in the whole document just to find a handful near the
// cursor, and this runs on every
// hover and every debounced completion keystroke. A single scan plus
// a slice (text[a:b] is O(1) — it shares the original backing array,
// no copy) does the same job with no per-line allocation at all.
func lineOffset(text string, n int) (start, end int, ok bool) {
  if n < 0 {
    return 0, 0, false
  }

  line := 0
  for i := 0; i < len(text); i++ {
    if text[i] != '\n' {
      continue
    }
    if line == n {
      return start, i, true
    }
    line++
    start = i + 1
  }

  if line == n {
    return start, len(text), true
  }
  return 0, 0, false
}
