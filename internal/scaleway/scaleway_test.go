package scaleway

import (
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "net/http/httptest"
  "strings"
  "sync"
  "testing"
  "time"
)

type fakeAPI struct {
  mu      sync.Mutex
  state   string
  actions []string
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  f.mu.Lock()
  defer f.mu.Unlock()
  if r.Header.Get("X-Auth-Token") != "secret" {
    w.WriteHeader(401)
    io.WriteString(w, `{"message":"authentication is denied","type":"denied_authentication"}`)

    return
  }

  base := "/instance/v1/zones/fr-par-2/servers/srv-1"
  switch {
  case r.Method == "GET" && r.URL.Path == base:
    ip := "null"
    if f.state == "running" {
      ip = `{"id":"ip-1","address":"51.15.0.9"}`
    }

    fmt.Fprintf(w, `{"server":{"id":"srv-1","state":%q,"public_ip":%s,"public_ips":[],"private_ip":null}}`, f.state, ip)
    if f.state == "starting" {
      f.state = "running"
    }
  case r.Method == "POST" && r.URL.Path == base + "/action":
    var body struct {
      Action string `json:"action"`
    }

    json.NewDecoder(r.Body).Decode(&body)
    f.actions = append(f.actions, body.Action)
    if body.Action == "poweron" {
      f.state = "starting"
    } else {
      f.state = "stopped"
    }

    io.WriteString(w, `{"task":{"id":"t-1","status":"pending"}}`)
  default:
    w.WriteHeader(404)
  }
}

func TestWaitRunningPowersOnAndOff(t *testing.T) {
  StatePoll = 10 * time.Millisecond
  f := &fakeAPI{ state: "stopped" }
  server := httptest.NewServer(f)
  defer server.Close()

  t.Setenv("SCW_SECRET_KEY", "secret")
  c, err := EnvClient("fr-par-2")
  if err != nil {
    t.Fatal(err)
  }

  c.Endpoint = server.URL

  ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
  defer cancel()
  srv, started, err := c.WaitRunning(ctx, "srv-1", nil)
  if err != nil {
    t.Fatalf("WaitRunning: %v", err)
  }

  if !started || srv.Host() != "51.15.0.9" {
    t.Fatalf("started=%v host=%q", started, srv.Host())
  }

  if err := c.PowerOff(ctx, "srv-1"); err != nil {
    t.Fatal(err)
  }

  if strings.Join(f.actions, ",") != "poweron,poweroff" {
    t.Fatalf("actions = %v", f.actions)
  }
}

func TestErrorsAreReadable(t *testing.T) {
  server := httptest.NewServer(&fakeAPI{ state: "running" })
  defer server.Close()
  c := &Client{ Zone: "fr-par-2", SecretKey: "wrong", HTTP: http.DefaultClient, Endpoint: server.URL }
  _, err := c.GetServer(context.Background(), "srv-1")
  if err == nil || !strings.Contains(err.Error(), "HTTP 401: authentication is denied") {
    t.Fatalf("err = %v", err)
  }
}

func TestNoCredentials(t *testing.T) {
  t.Setenv("SCW_SECRET_KEY", "")
  if _, err := EnvClient("fr-par-2"); err != ErrNoCredentials {
    t.Fatalf("err = %v", err)
  }
}
