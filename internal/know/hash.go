// The hash evidence keeps of the lines it cites. Each line is hashed
// on its own (FNV-1a), and a range's hash is a polynomial over its
// line hashes, mod 2³²:
//
//   H(l₁..lₙ) = h(l₁)·Bⁿ⁻¹ + h(l₂)·Bⁿ⁻² + … + h(lₙ)
//
// so a same-length window can be rolled down a file in O(1) per line
// to find cited code that moved (see Roll). It is only ever compared
// with the same evidence's earlier value, never used as an identity.

package know

// hashBase is the polynomial's base: odd, so multiplication by it is
// invertible mod 2³².
const hashBase = 1000003

// LineHash is FNV-1a over one line, without its newline.
func LineHash(line []byte) uint32 {
  h := uint32(2166136261)
  for _, c := range line {
    h ^= uint32(c)
    h *= 16777619
  }

  return h
}

// RangeHash combines line hashes into the hash of their range.
func RangeHash(lines []uint32) uint32 {
  h := uint32(0)
  for _, l := range lines {
    h = h * hashBase + l
  }

  return h
}

// LineHashes splits text into lines and hashes each.
func LineHashes(text []byte, buf []uint32) []uint32 {
  buf = buf[:0]
  start := 0
  for i, c := range text {
    if c == '\n' {
      buf = append(buf, LineHash(text[start:i]))
      start = i + 1
    }
  }

  if start < len(text) {
    buf = append(buf, LineHash(text[start:]))
  }

  return buf
}

// Roll finds where a window of n lines hashing to want starts in
// lines, rolling the hash down in O(len(lines)). It prefers the match
// nearest to near (the old start), since repeated code can match in
// several places; -1 when none does.
func Roll(lines []uint32, n int, want uint32, near int) int {
  if n <= 0 || n > len(lines) {
    return -1
  }

  // top is Bⁿ⁻¹: the weight of the line leaving the window
  top := uint32(1)
  for i := 1; i < n; i++ {
    top *= hashBase
  }

  best := -1
  h := RangeHash(lines[:n])
  for start := 0; ; start++ {
    if h == want && (best < 0 || abs(start - near) < abs(best - near)) {
      best = start
    }

    if start + n >= len(lines) {
      return best
    }

    h = (h - lines[start] * top) * hashBase + lines[start + n]
  }
}

func abs(x int) int {
  if x < 0 {
    return -x
  }

  return x
}
