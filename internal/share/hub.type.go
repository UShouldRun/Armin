package share

import (
  "context"
  "net"
  "sync"
)

// Hub serves a Host to the armins that join it, and sends each of
// them every Event.
type Hub struct {
  Instance int // this host's number among the projects' hosts

  host          Host
  ln            net.Listener
  path, numPath string

  mu    sync.Mutex
  conns map[*conn]struct{}
  seq   map[string]int // joined armins per kind, for origins
}

// conn is one joined armin.
type conn struct {
  c      net.Conn
  origin string

  writeMu sync.Mutex // one JSON line at a time

  jobsMu sync.Mutex
  jobs   map[uint32]context.CancelFunc // complete/explain in flight
  fills  map[string]uint32             // Fill.Key → its seq in flight
}
