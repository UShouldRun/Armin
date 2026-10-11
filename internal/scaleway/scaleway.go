// Package scaleway is the slice of the Scaleway Instance API armin
// needs to run a model on a server it powers on and off itself.
// Standard library only.
//
// poweroff, not stop_in_place: poweroff releases the hypervisor slot
// and stops compute billing; stop_in_place keeps the slot reserved.
package scaleway

import (
  "context"
  "encoding/json"
  "errors"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "os"
  "strings"
  "time"
)

// DefaultEndpoint is the Scaleway API host; the zone is in the path.
const DefaultEndpoint = "https://api.scaleway.com"

// CredentialEnv lists where the key comes from (Scaleway's own
// variable names) and what a Manager keeps out of child processes.
var CredentialEnv = []string{ "SCW_SECRET_KEY", "SCW_ACCESS_KEY" }

// ErrNoCredentials means SCW_SECRET_KEY isn't set.
var ErrNoCredentials = errors.New(
  "no Scaleway credentials: set SCW_SECRET_KEY (e.g. in " +
    "~/.config/armin/.env)",
)

// Client calls the Instance API in one zone (e.g. fr-par-2).
type Client struct {
  Zone, SecretKey string
  HTTP            *http.Client
  Endpoint        string // tests override; default DefaultEndpoint
}

// EnvClient builds a client from SCW_SECRET_KEY.
func EnvClient(zone string) (*Client, error) {
  key := os.Getenv("SCW_SECRET_KEY")
  if key == "" {
    return nil, ErrNoCredentials
  }

  return &Client{
    Zone:      zone,
    SecretKey: key,
    HTTP:      &http.Client{ Timeout: 30 * time.Second },
  }, nil
}

// Server is what GetServer reports.
type Server struct {
  ID, State           string
  PublicIP, PrivateIP string
}

// Host is the address to reach the server at.
func (s Server) Host() string {
  if s.PublicIP != "" {
    return s.PublicIP
  }

  return s.PrivateIP
}

type ipBody struct {
  Address string `json:"address"`
}

type serverBody struct {
  Server struct {
    ID        string    `json:"id"`
    State     string    `json:"state"`
    PublicIP  *ipBody   `json:"public_ip"`
    PublicIPs []*ipBody `json:"public_ips"`
    PrivateIP *string   `json:"private_ip"`
  } `json:"server"`
}

func (c *Client) serverURL(id, suffix string) string {
  endpoint := c.Endpoint
  if endpoint == "" {
    endpoint = DefaultEndpoint
  }

  return fmt.Sprintf(
    "%s/instance/v1/zones/%s/servers/%s%s",
    endpoint, url.PathEscape(c.Zone), url.PathEscape(id), suffix,
  )
}

func (c *Client) do(
  ctx context.Context, method, u string, body, out any,
) error {
  var rd io.Reader
  if body != nil {
    raw, _ := json.Marshal(body)
    rd = strings.NewReader(string(raw))
  }

  req, err := http.NewRequestWithContext(ctx, method, u, rd)
  if err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  req.Header.Set("X-Auth-Token", c.SecretKey)
  if body != nil {
    req.Header.Set("Content-Type", "application/json")
  }

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  defer resp.Body.Close()
  raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1 << 20))

  if resp.StatusCode >= 300 {
    var e struct {
      Message string `json:"message"`
    }

    msg := strings.TrimSpace(string(raw))
    if json.Unmarshal(raw, &e) == nil && e.Message != "" {
      msg = e.Message
    }

    return fmt.Errorf(
      "\n\t... in do:\n\t %s %s: HTTP %d: %s",
      method, u, resp.StatusCode, msg,
    )
  }

  if out == nil {
    return nil
  }

  if err := json.Unmarshal(raw, out); err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  return nil
}

// GetServer reports one server's state and addresses.
func (c *Client) GetServer(ctx context.Context, id string) (Server, error) {
  var b serverBody
  if err := c.do(ctx, http.MethodGet, c.serverURL(id, ""), nil, &b); err != nil {
    return Server{}, fmt.Errorf("\n\t... in GetServer:\n\t%w", err)
  }

  s := Server{ ID: b.Server.ID, State: b.Server.State }
  if b.Server.PublicIP != nil {
    s.PublicIP = b.Server.PublicIP.Address
  }

  for _, ip := range b.Server.PublicIPs {
    if s.PublicIP == "" && ip != nil {
      s.PublicIP = ip.Address
    }
  }

  if b.Server.PrivateIP != nil {
    s.PrivateIP = *b.Server.PrivateIP
  }

  return s, nil
}

// Action runs a server action: poweron, poweroff, reboot, ...
func (c *Client) Action(ctx context.Context, id, action string) error {
  err := c.do(
    ctx, http.MethodPost, c.serverURL(id, "/action"),
    map[string]string{ "action": action }, nil,
  )
  if err != nil {
    return fmt.Errorf("\n\t... in Action:\n\t %s: %w", action, err)
  }

  return nil
}

// PowerOff stops the server and releases its hypervisor slot.
func (c *Client) PowerOff(ctx context.Context, id string) error {
  return c.Action(ctx, id, "poweroff")
}

// StatePoll is how often WaitRunning re-reads (tests shorten it).
var StatePoll = 3 * time.Second

// WaitRunning polls until the server is running with an address,
// powering it on if it's stopped. started reports whether this call
// powered it on.
func (c *Client) WaitRunning(
  ctx context.Context, id string, status func(string),
) (srv Server, started bool, err error) {
  last := ""
  for {
    srv, err = c.GetServer(ctx, id)
    if err != nil {
      return srv, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", err)
    }

    if srv.State != last && status != nil {
      status(fmt.Sprintf("server %s is %s", id, srv.State))
    }

    last = srv.State

    switch srv.State {
    case "running":
      if srv.Host() != "" {
        return srv, started, nil
      }
    case "stopped", "stopped in place":
      if !started {
        if err := c.Action(ctx, id, "poweron"); err != nil {
          return srv, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", err)
        }

        started = true
      }
    case "locked":
      return srv, started, fmt.Errorf(
        "\n\t... in WaitRunning:\n\t server %s is locked by Scaleway", id,
      )
    }

    select {
    case <-ctx.Done():
      return srv, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", ctx.Err())
    case <-time.After(StatePoll):
    }
  }
}
