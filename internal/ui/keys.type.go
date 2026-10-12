package ui

// key is one decoded input event from a raw-mode terminal: either a
// printable rune, or one of the control keys below. Kept separate
// from the raw byte stream so the line-editing logic in
// lineinput.go can be tested without a real terminal — decodeKey is
// the only piece that has to deal with actual bytes.
type key struct {
  r       rune
  control controlKey
}

type controlKey int
