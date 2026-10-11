package llamacpp

import (
  "context"
  "fmt"
  "io"
  "net/http"
  "net/http/httptest"
  "os"
  "path/filepath"
  "strings"
  "sync"
  "testing"
  "time"

  "armin/internal/aws"
  "armin/internal/scaleway"
  "armin/internal/stackit"
)

type fakeEC2 struct {
  mu      sync.Mutex
  state   string
  actions []string
}

func (f *fakeEC2) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  raw, _ := io.ReadAll(r.Body)
  body := string(raw)
  f.mu.Lock()
  defer f.mu.Unlock()
  switch {
  case strings.Contains(body, "Action=DescribeInstances"):
    ip := ""
    if f.state == "running" {
      ip = "<ipAddress>203.0.113.7</ipAddress>"
    }

    fmt.Fprintf(w, `<R><reservationSet><item><instancesSet><item><instanceId>i-1</instanceId>`+
      `<instanceState><name>%s</name></instanceState>%s</item></instancesSet></item></reservationSet></R>`, f.state, ip)
    if f.state == "pending" {
      f.state = "running"
    }
  case strings.Contains(body, "Action=StartInstances"):
    f.actions = append(f.actions, "start")
    f.state = "pending"
  case strings.Contains(body, "Action=StopInstances"):
    f.actions = append(f.actions, "stop")
    f.state = "stopped"
  }
}

func (f *fakeEC2) log() string {
  f.mu.Lock()
  defer f.mu.Unlock()

  return strings.Join(f.actions, ",")
}

// fakeSSH records its argv and AWS env, then serves /health on the
// local end of its -L forward, like a tunnel to a ready llama-server.
const fakeSSH = `#!/bin/sh
echo "$@" > "$FAKE_SSH_DIR/args"
env | grep -E '^(AWS_|STACKIT_|SCW_)' > "$FAKE_SSH_DIR/env"
while [ $# -gt 0 ]; do
  if [ "$1" = "-L" ]; then port=$(echo "$2" | cut -d: -f2); fi
  shift
done
exec python3 -c "
import http.server
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(s):
        s.send_response(200); s.end_headers(); s.wfile.write(b'{}')
    def log_message(s, *a): pass
http.server.HTTPServer(('127.0.0.1', $port), H).serve_forever()"
`

func awsTestManager(t *testing.T, state string) (*Manager, *fakeEC2, string) {
  t.Helper()
  if _, err := os.Stat("/usr/bin/python3"); err != nil {
    t.Skip("python3 needed for the fake ssh")
  }

  aws.StatePoll = 10 * time.Millisecond

  dir := t.TempDir()
  ssh := filepath.Join(dir, "ssh")
  os.WriteFile(ssh, []byte(fakeSSH), 0o755)
  t.Setenv("FAKE_SSH_DIR", dir)
  t.Setenv("AWS_ACCESS_KEY_ID", "AKIDEXAMPLE")
  t.Setenv("AWS_SECRET_ACCESS_KEY", "secret")

  f := &fakeEC2{ state: state }
  server := httptest.NewServer(f)
  t.Cleanup(server.Close)

  m := New(map[string]ModelSpec{
    "qwen": {
      Binary:    "/opt/llama/llama-server",
      ModelPath: "/models/qwen 27b.gguf",
      Port:      freePort(t),
      Args:      []string{ "--jinja", "-c", "65536" },
      Remote: &RemoteSpec{
        Provider:         ProviderAWS,
        MachineID:        "i-1",
        Region:           "eu-west-1",
        SSHUser:          "ubuntu",
        SSHKey:           "/keys/k.pem",
        RemotePort:       8080,
        StopOnDisconnect: true,
      },
    },
  })
  m.SSHBinary, m.EC2Endpoint = ssh, server.URL

  return m, f, dir
}

func TestAWSModelStartsTunnelsAndStops(t *testing.T) {
  m, f, dir := awsTestManager(t, "stopped")

  var status []string
  m.OnStatus = func(s string) { status = append(status, s) }

  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  if _, err := m.Client(ctx, "qwen"); err != nil {
    t.Fatalf("Client: %v", err)
  }

  if f.log() != "start" {
    t.Fatalf("EC2 actions = %q, want start", f.log())
  }

  args, _ := os.ReadFile(filepath.Join(dir, "args"))
  for _, want := range []string{
    "-tt", "HostKeyAlias=i-1", "-i /keys/k.pem", "ubuntu@203.0.113.7",
    "127.0.0.1:8080", "trap 'sudo -n shutdown -h now' HUP;",
    "/opt/llama/llama-server -m '/models/qwen 27b.gguf' --host 127.0.0.1 --port 8080 --jinja -c 65536 --no-mmproj -np 1 -fa on -ctk q8_0 -ctv q8_0 -cram 1024",
  } {
    if !strings.Contains(string(args), want) {
      t.Errorf("ssh args missing %q:\n%s", want, args)
    }
  }

  if env, _ := os.ReadFile(filepath.Join(dir, "env")); len(env) > 0 {
    t.Errorf("AWS credentials leaked into ssh's environment: %s", env)
  }

  if len(status) == 0 || !strings.Contains(status[len(status) - 1], "ready") {
    t.Errorf("status = %v", status)
  }

  m.Shutdown(context.Background())
  if f.log() != "start,stop" {
    t.Fatalf("EC2 actions = %q, want start,stop", f.log())
  }
}

func TestAWSInstanceAlreadyRunningIsLeftRunning(t *testing.T) {
  m, f, dir := awsTestManager(t, "running")

  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  if _, err := m.Client(ctx, "qwen"); err != nil {
    t.Fatalf("Client: %v", err)
  }

  args, _ := os.ReadFile(filepath.Join(dir, "args"))
  if strings.Contains(string(args), "trap") || !strings.Contains(string(args), "exec /opt/llama") {
    t.Errorf("guard armed on an instance armin didn't start:\n%s", args)
  }

  m.Shutdown(context.Background())
  if f.log() != "" {
    t.Fatalf("EC2 actions = %q, want none", f.log())
  }
}

func TestAWSNeedsCredentials(t *testing.T) {
  m, f, _ := awsTestManager(t, "stopped")
  t.Setenv("AWS_ACCESS_KEY_ID", "")

  _, err := m.Client(context.Background(), "qwen")
  if err == nil || !strings.Contains(err.Error(), "AWS_ACCESS_KEY_ID") {
    t.Fatalf("err = %v", err)
  }

  if f.log() != "" {
    t.Fatalf("EC2 actions = %q, want none without credentials", f.log())
  }
}

func TestShellQuote(t *testing.T) {
  cases := map[string]string{
    "--jinja":           "--jinja",
    "/models/a.gguf":    "/models/a.gguf",
    "has space":         "'has space'",
    "it's":              `'it'\''s'`,
    "$(rm -rf /)":       "'$(rm -rf /)'",
    "":                  "''",
  }

  for in, want := range cases {
    if got := shellQuote(in); got != want {
      t.Errorf("shellQuote(%q) = %s, want %s", in, got, want)
    }
  }
}

type fakeIaaS struct {
  mu      sync.Mutex
  status  string
  actions []string
}

func (f *fakeIaaS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  f.mu.Lock()
  defer f.mu.Unlock()
  base := "/v2/projects/p-1/regions/eu01/servers/s-1"
  switch {
  case r.Method == "GET" && r.URL.Path == base:
    ip := ""
    if f.status == "ACTIVE" {
      ip = `,"publicIp":"192.0.2.10"`
    }

    fmt.Fprintf(w, `{"id":"s-1","status":%q,"nics":[{"ipv4":"10.0.0.4"%s}]}`, f.status, ip)
    if f.status == "STARTING" {
      f.status = "ACTIVE"
    }
  case r.Method == "POST" && strings.HasPrefix(r.URL.Path, base + "/"):
    action := strings.TrimPrefix(r.URL.Path, base + "/")
    f.actions = append(f.actions, action)
    if action == "start" {
      f.status = "STARTING"
    } else {
      f.status = "DEALLOCATED"
    }

    w.WriteHeader(202)
  }
}

func TestSTACKITModelStartsTunnelsAndDeallocates(t *testing.T) {
  m, _, dir := awsTestManager(t, "stopped")
  stackit.StatePoll = 10 * time.Millisecond
  t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "tok")

  f := &fakeIaaS{ status: "DEALLOCATED" }
  server := httptest.NewServer(f)
  defer server.Close()
  m.StackitEndpoint = server.URL

  spec := m.specs["qwen"]
  spec.Remote = &RemoteSpec{
    Provider:         ProviderSTACKIT,
    MachineID:        "s-1",
    Region:           "eu01",
    ProjectID:        "p-1",
    SSHUser:          "ubuntu",
    RemotePort:       8080,
    StopOnDisconnect: true,
  }

  m.specs["qwen"] = spec

  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  if _, err := m.Client(ctx, "qwen"); err != nil {
    t.Fatalf("Client: %v", err)
  }

  args, _ := os.ReadFile(filepath.Join(dir, "args"))
  if !strings.Contains(string(args), "ubuntu@192.0.2.10") || !strings.Contains(string(args), "HostKeyAlias=s-1") {
    t.Errorf("ssh args = %s", args)
  }

  if env, _ := os.ReadFile(filepath.Join(dir, "env")); len(env) > 0 {
    t.Errorf("credentials leaked: %s", env)
  }

  m.Shutdown(context.Background())
  f.mu.Lock()
  defer f.mu.Unlock()
  if strings.Join(f.actions, ",") != "start,deallocate" {
    t.Fatalf("actions = %v, want start,deallocate (stop keeps billing)", f.actions)
  }
}

func TestServerArgsLeanDefaults(t *testing.T) {
  const lean = "--no-mmproj -np 1 -fa on -ctk q8_0 -ctv q8_0 -cram 1024"
  cases := []struct {
    spec ModelSpec
    want string
  }{
    { ModelSpec{ Args: []string{ "-c", "8192" } }, "-c 8192 " + lean },
    { ModelSpec{ Args: []string{ "--mmproj", "v.gguf" } }, "--mmproj v.gguf -np 1 -fa on -ctk q8_0 -ctv q8_0 -cram 1024" },
    { ModelSpec{ Args: []string{ "-ctk", "f16", "--parallel=2" } }, "-ctk f16 --parallel=2 --no-mmproj -fa on -ctv q8_0 -cram 1024" },
    { ModelSpec{ Args: []string{ "-c", "8192" }, Flags: SpecVision }, "-c 8192 -np 1 -fa on -ctk q8_0 -ctv q8_0 -cram 1024" },
    { ModelSpec{ Args: []string{ "-c", "8192" }, Flags: SpecNoLean }, "-c 8192" },
  }

  for _, c := range cases {
    if got := strings.Join(serverArgs(c.spec), " "); got != c.want {
      t.Errorf("serverArgs(%v, flags=%b)\n got  %q\n want %q", c.spec.Args, c.spec.Flags, got, c.want)
    }
  }
}

type fakeScaleway struct {
  mu      sync.Mutex
  state   string
  actions []string
}

func (f *fakeScaleway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  f.mu.Lock()
  defer f.mu.Unlock()
  base := "/instance/v1/zones/fr-par-2/servers/srv-1"
  switch {
  case r.Method == "GET" && r.URL.Path == base:
    ip := "null"
    if f.state == "running" {
      ip = `{"address":"51.15.0.9"}`
    }

    fmt.Fprintf(w, `{"server":{"id":"srv-1","state":%q,"public_ip":%s}}`, f.state, ip)
    if f.state == "starting" {
      f.state = "running"
    }
  case r.Method == "POST" && r.URL.Path == base + "/action":
    raw, _ := io.ReadAll(r.Body)
    if strings.Contains(string(raw), `"poweron"`) {
      f.actions = append(f.actions, "poweron")
      f.state = "starting"
    } else {
      f.actions = append(f.actions, strings.Trim(strings.TrimPrefix(string(raw), `{"action":`), `"}`))
      f.state = "stopped"
    }
  }
}

func TestScalewayModelPowersOnTunnelsAndPowersOff(t *testing.T) {
  m, _, dir := awsTestManager(t, "stopped")
  scaleway.StatePoll = 10 * time.Millisecond
  t.Setenv("SCW_SECRET_KEY", "secret")

  f := &fakeScaleway{ state: "stopped" }
  server := httptest.NewServer(f)
  defer server.Close()
  m.ScalewayEndpoint = server.URL

  spec := m.specs["qwen"]
  spec.Remote = &RemoteSpec{
    Provider:         ProviderScaleway,
    MachineID:        "srv-1",
    Region:           "fr-par-2",
    SSHUser:          "root",
    RemotePort:       8080,
    StopOnDisconnect: true,
  }

  m.specs["qwen"] = spec

  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  if _, err := m.Client(ctx, "qwen"); err != nil {
    t.Fatalf("Client: %v", err)
  }

  args, _ := os.ReadFile(filepath.Join(dir, "args"))
  if !strings.Contains(string(args), "root@51.15.0.9") {
    t.Errorf("ssh args = %s", args)
  }

  if env, _ := os.ReadFile(filepath.Join(dir, "env")); len(env) > 0 {
    t.Errorf("credentials leaked: %s", env)
  }

  m.Shutdown(context.Background())
  f.mu.Lock()
  defer f.mu.Unlock()
  if strings.Join(f.actions, ",") != "poweron,poweroff" {
    t.Fatalf("actions = %v, want poweron,poweroff (stop_in_place keeps billing)", f.actions)
  }
}

func TestContextFlagHelpers(t *testing.T) {
  if n := contextOf([]string{ "--jinja", "-c", "98304" }); n != 98304 {
    t.Errorf("contextOf = %d", n)
  }

  if n := contextOf([]string{ "--ctx-size=4096" }); n != 4096 {
    t.Errorf("contextOf = %d", n)
  }

  got := strings.Join(withContext([]string{ "--jinja", "-c", "98304", "-ngl", "999" }, 196608), " ")
  if got != "--jinja -ngl 999 -c 196608" {
    t.Errorf("withContext = %q", got)
  }
}

func TestStreamTierSwitchRestartsServerNotMachine(t *testing.T) {
  m, f, dir := awsTestManager(t, "running")
  spec := m.specs["qwen"]
  spec.Args = []string{ "--jinja", "-c", "98304" }
  spec.Stream = &StreamSpec{
    Binary:   "/opt/kvstream/llama-server",
    Context:  196608,
    ArenaMiB: 2048,
    Args:     []string{ "--fit", "off" },
  }

  m.specs["qwen"] = spec

  w := m.Window("qwen")
  if w == nil || w.Resident() != 98304 || w.Max() != 196608 {
    t.Fatalf("Window = %+v", w)
  }

  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  if _, err := m.Client(ctx, "qwen"); err != nil {
    t.Fatalf("Client: %v", err)
  }

  if n, err := w.Grow(ctx); err != nil || n != 196608 || !w.Streaming() {
    t.Fatalf("Grow = %d, %v (streaming=%v)", n, err, w.Streaming())
  }

  args, _ := os.ReadFile(filepath.Join(dir, "args"))
  for _, want := range []string{
    "/opt/kvstream/llama-server", "-c 196608", "--kv-stream-arena-mib 2048", "--fit off",
  } {
    if !strings.Contains(string(args), want) {
      t.Errorf("stream-tier args missing %q:\n%s", want, args)
    }
  }

  if strings.Contains(string(args), "-c 98304") {
    t.Errorf("the resident window leaked into the stream tier:\n%s", args)
  }

  if n, err := w.Shrink(ctx); err != nil || n != 98304 || w.Streaming() {
    t.Fatalf("Shrink = %d, %v", n, err)
  }

  args, _ = os.ReadFile(filepath.Join(dir, "args"))
  if !strings.Contains(string(args), "/opt/llama/llama-server") || !strings.Contains(string(args), "-c 98304") {
    t.Errorf("resident-tier args = %s", args)
  }

  m.Shutdown(context.Background())
  if f.log() != "" {
    t.Fatalf("EC2 actions = %q: a tier switch must not touch the machine", f.log())
  }
}

func TestWindowNeedsStreamTierAndResidentContext(t *testing.T) {
  m := New(map[string]ModelSpec{
    "plain":   { Args: []string{ "-c", "8192" } },
    "nosize":  { Stream: &StreamSpec{ Context: 65536 } },
    "smaller": { Args: []string{ "-c", "8192" }, Stream: &StreamSpec{ Context: 4096 } },
  })
  defer m.Shutdown(context.Background())
  for _, name := range []string{ "plain", "nosize", "smaller", "missing" } {
    if m.Window(name) != nil {
      t.Errorf("Window(%q) should be nil", name)
    }
  }
}
