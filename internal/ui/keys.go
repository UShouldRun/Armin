package ui

import (
  "bufio"
  "unicode/utf8"
)

const (
  keyNone controlKey = iota
  keyEnter
  keyBackspace
  keyTab
  keyInterrupt // Ctrl-C
  keyEOF       // Ctrl-D
  keyKillLine  // Ctrl-U
  keyKillWord  // Ctrl-W
  keyIgnored   // a recognized-but-unsupported sequence (arrow keys)
  keyPaste     // a bracketed paste, its text in key.paste
)

// Bracketed paste: with it on, the terminal wraps pasted text in
// these, so a pasted newline reads as text, not as Enter.
const (
  pasteOn    = "\x1b[?2004h"
  pasteOff   = "\x1b[?2004l"
  pasteEnd   = "\x1b[201~"
  pasteLimit = 1 << 20 // bytes kept from one paste
)

// decodeKey reads one logical keypress from r: a single rune (ASCII
// or multi-byte UTF-8), or a recognized control key. Arrow keys and
// other CSI/SS3 escape sequences are consumed and reported as
// keyIgnored — armin's line editor doesn't support cursor movement,
// but it must not leak those raw bytes into the buffer as literal
// characters either.
func decodeKey(r *bufio.Reader) (key, error) {
  b, err := r.ReadByte()
  if err != nil {
    return key{}, err
  }

  switch b {
  case '\r', '\n':
    return key{ control: keyEnter }, nil
  case 127, 8: // DEL, Backspace
    return key{ control: keyBackspace }, nil
  case '\t':
    return key{ control: keyTab }, nil
  case 3: // Ctrl-C
    return key{ control: keyInterrupt }, nil
  case 4: // Ctrl-D
    return key{ control: keyEOF }, nil
  case 21: // Ctrl-U
    return key{ control: keyKillLine }, nil
  case 23: // Ctrl-W
    return key{ control: keyKillWord }, nil
  case 0x1b: // ESC
    return decodeEscape(r)
  }

  if b < 0x20 {
    return key{ control: keyIgnored }, nil // other control bytes
  }

  if b < 0x80 {
    return key{ r: rune(b) }, nil
  }

  return decodeUTF8Rest(r, b)
}

func decodeUTF8Rest(r *bufio.Reader, first byte) (key, error) {
  n := utf8SeqLen(first)
  buf := make([]byte, n)
  buf[0] = first
  for i := 1; i < n; i++ {
    b, err := r.ReadByte()
    if err != nil {
      return key{}, err
    }

    buf[i] = b
  }

  ru, _ := utf8.DecodeRune(buf)

  return key{ r: ru }, nil
}

func utf8SeqLen(b byte) int {
  switch {
  case b & 0xE0 == 0xC0:
    return 2
  case b & 0xF0 == 0xE0:
    return 3
  case b & 0xF8 == 0xF0:
    return 4
  default:
    return 1
  }
}

// decodeEscape consumes one CSI ("\x1b[...") or SS3 ("\x1bO...")
// sequence and reports keyIgnored, except a paste's start, which
// reads the whole paste. A bare ESC with nothing following (the user
// pressing Escape itself) is also keyIgnored, not an error.
func decodeEscape(r *bufio.Reader) (key, error) {
  b1, err := r.ReadByte()
  if err != nil {
    return key{ control: keyIgnored }, nil
  }

  if b1 != '[' && b1 != 'O' {
    return key{ control: keyIgnored }, nil
  }

  var params [8]byte
  n := 0
  for {
    b2, err := r.ReadByte()
    if err != nil {
      return key{ control: keyIgnored }, nil
    }

    if b2 >= 0x40 && b2 <= 0x7e {
      if b1 == '[' && b2 == '~' && string(params[:n]) == "200" {
        return decodePaste(r)
      }

      return key{ control: keyIgnored }, nil
    }

    if n < len(params) {
      params[n] = b2
      n++
    }
  }
}

// decodePaste reads up to the paste's end marker. Line endings come
// out as '\n', tabs stay, other control bytes are dropped: they'd
// throw off the line editor's columns.
func decodePaste(r *bufio.Reader) (key, error) {
  var raw []byte
  match := 0 // bytes of pasteEnd seen so far
  for match < len(pasteEnd) {
    b, err := r.ReadByte()
    if err != nil {
      return key{}, err
    }

    if b == pasteEnd[match] {
      match++
      continue
    }

    // Past the limit, the rest is read to the end but not kept.
    if len(raw) < pasteLimit {
      raw = append(raw, pasteEnd[:match]...)
    }

    match = 0
    if b == pasteEnd[0] {
      match = 1
    } else if len(raw) < pasteLimit {
      raw = append(raw, b)
    }
  }

  text := make([]rune, 0, len(raw))
  for i := 0; i < len(raw); {
    ru, size := utf8.DecodeRune(raw[i:])
    i += size

    switch {
    case ru == '\r':
      if i < len(raw) && raw[i] == '\n' {
        i++
      }

      text = append(text, '\n')
    case ru == '\n', ru == '\t', ru >= 0x20 && ru != 0x7f:
      text = append(text, ru)
    }
  }

  return key{ control: keyPaste, paste: text }, nil
}
