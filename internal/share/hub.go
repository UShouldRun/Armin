// The host's side of the socket. Listen claims the project (or
// reports ErrHosted); each joined armin gets a goroutine reading its
// Requests and answering through Host, and Emit fans the conversation's
// Events out to all of them, dropping any that stop reading.

package share

import (
  "bufio"
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "net"
  "os"
  "path/filepath"
  "strconv"
  "time"
)

// writeTimeout drops a joined armin that stops reading rather than
// let it stall the chat for everyone.
const writeTimeout = 2 * time.Second

// ErrHosted: another armin already hosts this project.
var ErrHosted = errors.New("another armin already hosts this project")

// Listen makes host the project's instance: armins started in root
// later join it. ErrHosted when one already does.
func Listen(ctx context.Context, root string, host Host) (*Hub, error) {
  path, err := Socket(root)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Listen:\n\t%w", err)
  }

  if live(path) {
    return nil, ErrHosted
  }

  dir := filepath.Dir(path)
  err = os.MkdirAll(dir, 0o700)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Listen:\n\t%w", err)
  }

  os.Remove(path) // stale, from an armin that died
  ln, err := net.Listen("unix", path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Listen:\n\t%w", err)
  }

  os.Chmod(path, 0o600)

  h := &Hub{
    Instance: pickInstance(dir),
    host:     host,
    ln:       ln,
    path:     path,
    numPath:  path[:len(path) - len(".sock")] + ".instance",
    conns:    make(map[*conn]struct{}),
    seq:      make(map[string]int),
  }

  os.WriteFile(h.numPath, []byte(strconv.Itoa(h.Instance)), 0o600)

  go func() {
    for {
      c, err := ln.Accept()
      if err != nil {
        return
      }

      go h.serve(ctx, c)
    }
  }()

  return h, nil
}

// Close stops serving and removes the socket. Safe on a nil Hub.
func (h *Hub) Close() {
  if h == nil {
    return
  }

  h.ln.Close()
  os.Remove(h.path)
  os.Remove(h.numPath)

  h.mu.Lock()
  for c := range h.conns {
    c.c.Close()
  }

  h.mu.Unlock()
}

// Emit sends e to every joined armin, dropping any that fail. Safe
// on a nil Hub.
func (h *Hub) Emit(e Event) {
  if h == nil {
    return
  }

  h.mu.Lock()
  conns := make([]*conn, 0, len(h.conns))
  for c := range h.conns {
    conns = append(conns, c)
  }

  h.mu.Unlock()

  if len(conns) == 0 {
    return
  }

  line, err := json.Marshal(e)
  if err != nil {
    return
  }

  line = append(line, '\n')
  for _, c := range conns {
    err = c.write(line)
    if err != nil {
      h.drop(c)
    }
  }
}

func (c *conn) write(line []byte) error {
  c.writeMu.Lock()
  defer c.writeMu.Unlock()

  c.c.SetWriteDeadline(time.Now().Add(writeTimeout))
  _, err := c.c.Write(line)

  return err
}

func (c *conn) send(e Event) error {
  line, err := json.Marshal(e)
  if err != nil {
    return err
  }

  return c.write(append(line, '\n'))
}

func (h *Hub) drop(c *conn) {
  h.mu.Lock()
  delete(h.conns, c)
  h.mu.Unlock()
  c.c.Close()

  c.jobsMu.Lock()
  for _, cancel := range c.jobs {
    cancel()
  }

  c.jobsMu.Unlock()
}

// serve is one joined armin: it says what it is ("hello", Text: its
// kind), gets its origin, the instance, the models and the history,
// then sends Requests.
func (h *Hub) serve(ctx context.Context, nc net.Conn) {
  scan := bufio.NewScanner(nc)
  scan.Buffer(make([]byte, 0, 4096), 4 << 20)
  if !scan.Scan() {
    nc.Close()
    return
  }

  var hello Request
  json.Unmarshal(scan.Bytes(), &hello)
  kind := hello.Text
  if kind == "" {
    kind = "join"
  }

  h.mu.Lock()
  h.seq[kind]++
  c := &conn{
    c:      nc,
    origin: kind + ":" + strconv.Itoa(h.seq[kind] + 1),
    jobs:   make(map[uint32]context.CancelFunc),
    fills:  make(map[string]uint32),
  }

  h.conns[c] = struct{}{}
  h.mu.Unlock()
  defer h.drop(c)

  info := h.host.Models()
  info.Instance = h.Instance
  c.send(Event{ Type: "hello", Delta: c.origin, Instance: h.Instance, Model: &info })
  c.send(Event{ Type: "history", History: h.host.History() })

  for scan.Scan() {
    var req Request
    if json.Unmarshal(scan.Bytes(), &req) != nil {
      continue
    }

    h.handle(ctx, c, req)
  }
}

func (h *Hub) handle(ctx context.Context, c *conn, req Request) {
  reply := func(e Event) {
    if req.Seq == 0 {
      return
    }

    e.Type, e.Reply = "reply", req.Seq
    c.send(e)
  }

  fail := func(err error) { reply(Event{ Error: err.Error() }) }

  switch req.Op {
  case "ask":
    go func() {
      err := h.host.Ask(ctx, req.Text, req.File, c.origin)
      if errors.Is(err, ErrBusy) {
        c.send(Event{ Type: "error", Delta: err.Error(), Origin: c.origin })
      }
    }()

  case "cancel":
    h.host.Stop()

  case "clear":
    err := h.host.Clear(c.origin)
    if err != nil {
      fail(err)
      return
    }

    reply(Event{})

  case "history":
    reply(Event{ History: h.host.History() })

  case "model":
    info := h.host.Models()
    if req.Text != "" {
      var err error
      info, err = h.host.SwitchModel(ctx, req.Text)
      if err != nil {
        fail(err)
        return
      }
    }

    info.Instance = h.Instance
    reply(Event{ Model: &info })

  case "complete":
    if req.Fill == nil {
      fail(errors.New("complete without fill"))
      return
    }

    jobCtx := c.start(ctx, req.Seq, req.Fill.Key)
    go func() {
      defer c.end(req.Seq, req.Fill.Key)
      text, err := h.host.Complete(jobCtx, *req.Fill)
      if err != nil {
        fail(err)
        return
      }

      reply(Event{ Delta: text })
    }()

  case "explain":
    jobCtx := c.start(ctx, req.Seq, "")
    go func() {
      defer c.end(req.Seq, "")
      text, err := h.host.Explain(jobCtx, req.Text)
      if err != nil {
        fail(err)
        return
      }

      reply(Event{ Delta: text })
    }()

  case "drop":
    c.jobsMu.Lock()
    cancel := c.jobs[req.Seq]
    c.jobsMu.Unlock()
    if cancel != nil {
      cancel()
    }
  }
}

// start registers a job; a fill supersedes the one before it with
// the same key (its result is stale the moment a newer one exists).
func (c *conn) start(ctx context.Context, seq uint32, key string) context.Context {
  jobCtx, cancel := context.WithCancel(ctx)

  c.jobsMu.Lock()
  defer c.jobsMu.Unlock()

  if key != "" {
    prev, ok := c.fills[key]
    if ok && c.jobs[prev] != nil {
      c.jobs[prev]()
    }

    c.fills[key] = seq
  }

  c.jobs[seq] = cancel

  return jobCtx
}

func (c *conn) end(seq uint32, key string) {
  c.jobsMu.Lock()
  defer c.jobsMu.Unlock()

  cancel := c.jobs[seq]
  if cancel != nil {
    cancel()
  }

  delete(c.jobs, seq)
  if key != "" && c.fills[key] == seq {
    delete(c.fills, key)
  }
}
