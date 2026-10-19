// The types of context.go.

package agent

// elidedStub replaces a tool result elide dropped. Elided comes
// first, so a stub starts with elidedPrefix and is never re-elided.
type elidedStub struct {
  Elided   string `json:"elided"`
  Bytes    int    `json:"bytes"`
  Archived string `json:"archived,omitempty"`
  Note     string `json:"note,omitempty"`
}
