// A joined armin's side of the socket: Dial finds the host for the
// project (or a parent folder), then each method is a Request, and
// calls wait for the reply matching their sequence number.

package share

import (
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "net"
  "path/filepath"
  "sync/atomic"
)

// ErrGone: the host went away.
var ErrGone = errors.New("the armin instance this one joined went away")

// Dial joins the host of root or of the nearest parent that has one
// (nvim roots its armin at the repository), as kind ("terminal",
// "nvim"). Fails fast when none listens.
func Dial(root, kind string) (*Client, error) {
  dir := filepath.Clean(root)
  for {
    path, err := Socket(dir)
    if err != nil {
      return nil, err
    }

    nc, err := net.DialTimeout("unix", path, dialTimeout)
    if err == nil {
      return join(nc, kind)
    }

    parent := filepath.Dir(dir)
    if parent == dir {
      return nil, err
    }

    dir = parent
  }
}

func join(nc net.Conn, kind string) (*Client, error) {
  c := &Client{
    Events:  make(chan Event, 1024),
    conn:    nc,
    pending: make(map[uint32]chan Event),
    done:    make(chan struct{}),
  }

  err := c.Send(Request{ Op: "hello", Text: kind })
  if err != nil {
    nc.Close()
    return nil, fmt.Errorf("\n\t... in join:\n\t%w", err)
  }

  dec := json.NewDecoder(nc)
  var hello, history Event
  err = dec.Decode(&hello)
  if err == nil {
    err = dec.Decode(&history)
  }

  if err != nil {
    nc.Close()
    return nil, fmt.Errorf("\n\t... in join:\n\t%w", err)
  }

  c.Origin, c.Instance, c.History = hello.Delta, hello.Instance, history.History
  if hello.Model != nil {
    c.Model = *hello.Model
  }

  go c.read(dec)

  return c, nil
}

// read routes replies to their callers and everything else to
// Events.
func (c *Client) read(dec *json.Decoder) {
  defer func() {
    close(c.done)
    close(c.Events)
  }()

  for {
    var e Event
    err := dec.Decode(&e)
    if err != nil {
      return
    }

    if e.Type != "reply" {
      c.Events <- e
      continue
    }

    c.mu.Lock()
    ch := c.pending[e.Reply]
    delete(c.pending, e.Reply)
    c.mu.Unlock()
    if ch != nil {
      ch <- e
    }
  }
}

func (c *Client) Close() { c.conn.Close() }

// Gone is closed once the host went away.
func (c *Client) Gone() <-chan struct{} { return c.done }

// Send sends a request without waiting for anything.
func (c *Client) Send(r Request) error {
  line, err := json.Marshal(r)
  if err != nil {
    return err
  }

  c.mu.Lock()
  defer c.mu.Unlock()
  _, err = c.conn.Write(append(line, '\n'))

  return err
}

// call sends r and waits for its reply. A canceled ctx drops the
// job host-side too.
func (c *Client) call(ctx context.Context, r Request) (Event, error) {
  r.Seq = atomic.AddUint32(&c.seq, 1)
  ch := make(chan Event, 1)

  c.mu.Lock()
  c.pending[r.Seq] = ch
  c.mu.Unlock()

  err := c.Send(r)
  if err != nil {
    return Event{}, ErrGone
  }

  select {
  case e := <-ch:
    if e.Error != "" {
      return e, errors.New(e.Error)
    }

    return e, nil
  case <-ctx.Done():
    c.mu.Lock()
    delete(c.pending, r.Seq)
    c.mu.Unlock()
    c.Send(Request{ Op: "drop", Seq: r.Seq })

    return Event{}, ctx.Err()
  case <-c.done:
    return Event{}, ErrGone
  }
}

// Ask starts a turn; its events, and a busy "error", come through
// Events.
func (c *Client) Ask(question, file string) error {
  return c.Send(Request{ Op: "ask", Text: question, File: file })
}

// Stop stops the turn in flight, whoever asked.
func (c *Client) Stop() { c.Send(Request{ Op: "cancel" }) }

func (c *Client) Clear(ctx context.Context) error {
  _, err := c.call(ctx, Request{ Op: "clear" })
  return err
}

func (c *Client) Lines(ctx context.Context) ([]ChatLine, error) {
  e, err := c.call(ctx, Request{ Op: "history" })
  return e.History, err
}

func (c *Client) Models(ctx context.Context) (ModelInfo, error) {
  return c.model(ctx, "")
}

func (c *Client) SwitchModel(ctx context.Context, name string) (ModelInfo, error) {
  return c.model(ctx, name)
}

func (c *Client) model(ctx context.Context, name string) (ModelInfo, error) {
  e, err := c.call(ctx, Request{ Op: "model", Text: name })
  if err != nil || e.Model == nil {
    return ModelInfo{}, err
  }

  return *e.Model, nil
}

func (c *Client) Complete(ctx context.Context, f Fill) (string, error) {
  e, err := c.call(ctx, Request{ Op: "complete", Fill: &f })
  return e.Delta, err
}

func (c *Client) Explain(ctx context.Context, question string) (string, error) {
  e, err := c.call(ctx, Request{ Op: "explain", Text: question })
  return e.Delta, err
}
