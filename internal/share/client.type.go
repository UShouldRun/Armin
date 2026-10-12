package share

import (
  "net"
  "sync"
)

// Client is a joined armin's end of the socket.
type Client struct {
  Origin   string    // how the host names this armin's questions
  Instance int       // the host's number
  Model    ModelInfo // at join time
  History  []ChatLine

  // Events is every Event but replies, in order; closed when the
  // host goes away.
  Events chan Event

  conn    net.Conn
  mu      sync.Mutex
  pending map[uint32]chan Event
  seq     uint32
  done    chan struct{}
}
