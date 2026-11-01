// Package tokens splits text into the searchable terms both BM25
// indexes use (the reference library and the knowledge DAGs), so a
// query matches the same way in each: lowercase runs of letters,
// digits and "_", 2 to 32 bytes, no stopwords, a plural "s" dropped.
package tokens

import (
  "unicode"
  "unicode/utf8"
)

// stopwords are too common to help a search; leaving them out of
// the index makes it a good deal smaller.
var stopwords = map[string]bool{
  "the": true, "of": true, "and": true, "to": true, "in": true,
  "is": true, "it": true, "that": true, "for": true, "as": true,
  "on": true, "with": true, "be": true, "by": true, "this": true,
  "are": true, "or": true, "an": true, "at": true, "from": true,
  "which": true, "we": true, "can": true, "if": true, "not": true,
  "was": true, "but": true, "have": true, "has": true, "will": true,
  "their": true, "there": true, "these": true, "they": true, "its": true,
  "you": true, "your": true, "all": true, "also": true, "so": true,
}

// Each calls emit with every searchable term of s: lowercase
// runs of letters, digits and "_", 2 to 32 bytes, no stopwords, a
// plural "s" dropped. Allocation-free for ASCII text.
func Each(s string, buf []byte, emit func(term []byte)) {
  buf = buf[:0]
  for i := 0; i <= len(s); {
    if i < len(s) {
      c := s[i]
      if c < utf8.RuneSelf {
        if c >= 'A' && c <= 'Z' {
          c += 'a' - 'A'
        }

        if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' {
          buf = append(buf, c)
          i++
          continue
        }

        i++
      } else {
        r, size := utf8.DecodeRuneInString(s[i:])
        if unicode.IsLetter(r) || unicode.IsDigit(r) {
          buf = utf8.AppendRune(buf, unicode.ToLower(r))
          i += size
          continue
        }

        i += size
      }
    } else {
      i++
    }

    if len(buf) >= 2 && len(buf) <= 32 && keep(buf) {
      emit(stem(buf))
    }

    buf = buf[:0]
  }
}

func keep(t []byte) bool {
  if stopwords[string(t)] {
    return false
  }

  digits := 0
  for _, c := range t {
    if c >= '0' && c <= '9' {
      digits++
    }
  }

  return digits < 7 // long numbers: page refs, hashes, tables
}

// stem drops a plural "s": "mutexes" and "mutex" still differ, but
// "threads" finds "thread". The same on both sides, so consistent.
func stem(t []byte) []byte {
  n := len(t)
  if n <= 4 || t[n - 1] != 's' {
    return t
  }

  switch t[n - 2] {
  case 's', 'u', 'i': // class, status, analysis
    return t
  }

  return t[:n - 1]
}

// Query is Each for a query, without duplicates.
func Query(q string) []string {
  var out []string
  seen := map[string]bool{}
  Each(q, make([]byte, 0, 32), func(t []byte) {
    s := string(t)
    if !seen[s] {
      seen[s] = true
      out = append(out, s)
    }
  })

  return out
}
