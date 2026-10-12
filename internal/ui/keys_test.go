package ui

import (
  "bufio"
  "strings"
  "testing"
)

func decode(t *testing.T, s string) key {
  t.Helper()
  k, err := decodeKey(bufio.NewReader(strings.NewReader(s)))
  if err != nil {
    t.Fatalf("decodeKey(%q): %v", s, err)
  }
  return k
}

func TestDecodeKeyPrintableASCII(t *testing.T) {
  k := decode(t, "a")
  if k.control != keyNone || k.r != 'a' {
    t.Fatalf("decodeKey(a) = %+v", k)
  }
}

func TestDecodeKeyControlKeys(t *testing.T) {
  cases := map[string]controlKey{
    "\r":        keyEnter,
    "\n":        keyEnter,
    "\x7f":      keyBackspace,
    "\b":        keyBackspace,
    "\t":        keyTab,
    "\x03":      keyInterrupt,
    "\x04":      keyEOF,
  }
  for input, want := range cases {
    k := decode(t, input)
    if k.control != want {
      t.Errorf("decodeKey(%q) = %+v, want control %v", input, k, want)
    }
  }
}

func TestDecodeKeyMultiByteUTF8(t *testing.T) {
  // "é" is U+00E9, encoded as two UTF-8 bytes.
  k := decode(t, "é")
  if k.control != keyNone || k.r != 'é' {
    t.Fatalf("decodeKey(é) = %+v", k)
  }
}

func TestDecodeKeyArrowKeysAreIgnored(t *testing.T) {
  // Up arrow: ESC [ A.
  k := decode(t, "\x1b[A")
  if k.control != keyIgnored {
    t.Fatalf("decodeKey(up arrow) = %+v, want keyIgnored", k)
  }
}

func TestDecodeKeyBareEscapeIsIgnored(t *testing.T) {
  k := decode(t, "\x1b")
  if k.control != keyIgnored {
    t.Fatalf("decodeKey(bare ESC) = %+v, want keyIgnored", k)
  }
}

func TestDecodeKeyEOF(t *testing.T) {
  r := bufio.NewReader(strings.NewReader(""))
  _, err := decodeKey(r)
  if err == nil {
    t.Fatal("expected an error reading from an empty stream")
  }
}

func TestDecodeKeySequential(t *testing.T) {
  r := bufio.NewReader(strings.NewReader("ab"))

  first, err := decodeKey(r)
  if err != nil || first.r != 'a' {
    t.Fatalf("first key = %+v, err = %v", first, err)
  }
  second, err := decodeKey(r)
  if err != nil || second.r != 'b' {
    t.Fatalf("second key = %+v, err = %v", second, err)
  }
}
