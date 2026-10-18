// A fill-in-the-middle window: the text before and after the cursor.

package lspserver

// fim is one fill-in-the-middle request cut out of a document.
type fim struct {
  head, prefix, prompt, suffix string
  rest, next                   string // after the cursor; the next non-blank line
  indent                       int
}
