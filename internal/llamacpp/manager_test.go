package llamacpp

import (
  "context"
  "fmt"
  "io"
  "net"
  "net/http"
  "os"
  "os/exec"
  "path/filepath"
  "testing"
  "time"
)

func TestModelSpecManaged(t *testing.T) {
  if (ModelSpec{ URL: "http://x" }).managed() {
    t.Fatal("a spec with a URL should not be managed")
  }
  if !(ModelSpec{ Binary: "llama-server" }).managed() {
    t.Fatal("a spec without a URL should be managed")
  }
}

func TestModelSpecIdleTimeout(t *testing.T) {
  if got := (ModelSpec{}).idleTimeout(); got != DefaultIdleTimeout {
    t.Fatalf("zero IdleTimeout = %v, want DefaultIdleTimeout", got)
  }
  if got := (ModelSpec{ IdleTimeout: 5 * time.Minute }).idleTimeout(); got != 5*time.Minute {
    t.Fatalf("idleTimeout() = %v, want the override", got)
  }
  if got := (ModelSpec{ IdleTimeout: -1 }).idleTimeout(); got >= 0 {
    t.Fatalf("idleTimeout() = %v, want a negative sentinel preserved", got)
  }
}

func TestModelSpecURL(t *testing.T) {
  if got := (ModelSpec{ URL: "http://elsewhere:9000" }).url(); got != "http://elsewhere:9000" {
    t.Fatalf("url() = %q, want the external URL unchanged", got)
  }
  if got := (ModelSpec{ Binary: "llama-server", Port: 8081 }).url(); got != "http://127.0.0.1:8081" {
    t.Fatalf("url() = %q, want the local managed port", got)
  }
}

func TestHasAndNames(t *testing.T) {
  m := New(map[string]ModelSpec{
    "chat": { URL: "http://localhost:8080" },
    "fim":  { Binary: "llama-server", Port: 8081 },
  })
  defer m.Shutdown(context.Background())

  if !m.Has("chat") || !m.Has("fim") || m.Has("nope") {
    t.Fatal("Has didn't match the configured specs")
  }

  names := m.Names()
  if len(names) != 2 {
    t.Fatalf("Names() = %+v, want 2 entries", names)
  }
  for _, s := range names {
    if s.Running {
      t.Fatalf("%+v: nothing should be running before first use", s)
    }
  }
}

func TestClientUnconfiguredModelErrors(t *testing.T) {
  m := New(nil)
  defer m.Shutdown(context.Background())

  if _, err := m.Client(context.Background(), "nope"); err == nil {
    t.Fatal("expected an error for an unconfigured model name")
  }
}

func TestClientExternalSpecNeverSpawns(t *testing.T) {
  m := New(map[string]ModelSpec{ "chat": { URL: "http://example.invalid:9" } })
  defer m.Shutdown(context.Background())

  client, err := m.Client(context.Background(), "chat")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  if client.BaseURL != "http://example.invalid:9" {
    t.Fatalf("BaseURL = %q, want the external spec's URL untouched", client.BaseURL)
  }

  m.mu.Lock()
  _, tracked := m.procs["chat"]
  m.mu.Unlock()
  if tracked {
    t.Fatal("an externally managed spec must never be tracked as a spawned process")
  }
}

func TestClientCarriesAPIKeyFromSpec(t *testing.T) {
  t.Setenv("ARMIN_TEST_API_KEY", "sk-ant-test")
  m := New(map[string]ModelSpec{
    "claude": { URL: "https://api.anthropic.com/v1", APIKeyEnv: "ARMIN_TEST_API_KEY" },
  })
  defer m.Shutdown(context.Background())

  client, err := m.Client(context.Background(), "claude")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  if client.APIKey != "sk-ant-test" {
    t.Fatalf("Client.APIKey = %q, want it resolved from APIKeyEnv", client.APIKey)
  }
}

func TestSwitchUpdatesAPIKeyOnTheSharedClient(t *testing.T) {
  t.Setenv("ARMIN_TEST_API_KEY", "sk-ant-test")
  m := New(map[string]ModelSpec{
    "local":  { URL: "http://localhost:8080" },
    "claude": { URL: "https://api.anthropic.com/v1", APIKeyEnv: "ARMIN_TEST_API_KEY" },
  })
  defer m.Shutdown(context.Background())

  client, err := m.Client(context.Background(), "local")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  if client.APIKey != "" {
    t.Fatalf("Client.APIKey = %q, want empty for a spec with no APIKeyEnv", client.APIKey)
  }

  if err := m.Switch(context.Background(), client, "claude"); err != nil {
    t.Fatalf("Switch: %v", err)
  }
  if client.APIKey != "sk-ant-test" {
    t.Fatalf("after Switch, Client.APIKey = %q, want sk-ant-test", client.APIKey)
  }
}

func TestTouchUpdatesRefcountAndLastUsed(t *testing.T) {
  m := New(map[string]ModelSpec{ "fim": { Binary: "x", Port: 1 } })
  defer m.Shutdown(context.Background())

  old := time.Now().Add(-time.Hour)
  m.mu.Lock()
  m.procs["fim"] = &process{ cmd: exec.Command("true"), lastUsed: old }
  m.mu.Unlock()

  m.touch("fim", 1)
  m.mu.Lock()
  refcount, lastUsed := m.procs["fim"].refcount, m.procs["fim"].lastUsed
  m.mu.Unlock()
  if refcount != 1 {
    t.Fatalf("refcount = %d, want 1", refcount)
  }
  if !lastUsed.After(old) {
    t.Fatal("touch should bump lastUsed")
  }

  m.touch("fim", -1)
  m.mu.Lock()
  refcount = m.procs["fim"].refcount
  m.mu.Unlock()
  if refcount != 0 {
    t.Fatalf("refcount after release = %d, want 0", refcount)
  }
}

func TestTouchOnExternalSpecIsNoop(t *testing.T) {
  m := New(map[string]ModelSpec{ "chat": { URL: "http://x" } })
  defer m.Shutdown(context.Background())

  m.touch("chat", 1) // must not panic — "chat" has no procs entry
}

// TestReapIdleSkipsInFlightAndFreshEntries constructs process
// entries directly (never actually spawned: cmd.Process stays nil,
// so stopProcess's nil-guard makes reaping them a safe no-op) to
// test reapIdle's selection logic in isolation from real subprocess
// spawning.
func TestReapIdleSelection(t *testing.T) {
  m := New(map[string]ModelSpec{
    "idle-long-enough": { Binary: "x", Port: 1, IdleTimeout: time.Minute },
    "idle-too-recent":  { Binary: "x", Port: 2, IdleTimeout: time.Hour },
    "busy":              { Binary: "x", Port: 3, IdleTimeout: time.Minute },
    "never-idle":        { Binary: "x", Port: 4, IdleTimeout: -1 },
  })
  defer m.Shutdown(context.Background())

  m.mu.Lock()
  m.procs["idle-long-enough"] = &process{ cmd: exec.Command("true"), lastUsed: time.Now().Add(-2 * time.Minute) }
  m.procs["idle-too-recent"] = &process{ cmd: exec.Command("true"), lastUsed: time.Now().Add(-2 * time.Minute) }
  m.procs["busy"] = &process{ cmd: exec.Command("true"), lastUsed: time.Now().Add(-2 * time.Minute), refcount: 1 }
  m.procs["never-idle"] = &process{ cmd: exec.Command("true"), lastUsed: time.Now().Add(-24 * time.Hour) }
  m.mu.Unlock()

  m.reapIdle()

  m.mu.Lock()
  _, longEnoughStillTracked := m.procs["idle-long-enough"]
  _, tooRecentStillTracked := m.procs["idle-too-recent"]
  _, busyStillTracked := m.procs["busy"]
  _, neverIdleStillTracked := m.procs["never-idle"]
  m.mu.Unlock()

  if longEnoughStillTracked {
    t.Error("idle-long-enough should have been reaped")
  }
  if !tooRecentStillTracked {
    t.Error("idle-too-recent hasn't hit its (longer) timeout yet, should survive")
  }
  if !busyStillTracked {
    t.Error("busy has refcount>0, must never be reaped regardless of idle time")
  }
  if !neverIdleStillTracked {
    t.Error("never-idle has a negative IdleTimeout, must never be reaped")
  }
}

// --- end-to-end: real spawn, health poll, and shutdown ------------
//
// fakeLlamaServer builds a tiny shell wrapper around this same test
// binary, re-invoked with GO_WANT_HELPER_PROCESS=1 so it runs
// TestHelperProcess instead of the real test suite — the same
// os.Args[0]-as-subprocess pattern os/exec's own tests use, letting
// Manager.spawn() exec something real without depending on an
// actual llama-server binary being installed.

func TestHelperProcess(t *testing.T) {
  if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
    return
  }
  defer os.Exit(0)

  var port string
  for i, a := range os.Args {
    if a == "--port" && i+1 < len(os.Args) {
      port = os.Args[i+1]
    }
  }
  if port == "" {
    fmt.Fprintln(os.Stderr, "helper: no --port given")
    os.Exit(1)
  }

  mux := http.NewServeMux()
  mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
  })
  // /env reports whether this process (not the test process) sees a
  // given environment variable — TestSpawnScrubsSecretEnvVars uses
  // it to confirm spawn() actually strips configured secrets rather
  // than just trusting cmd.Env's construction in isolation.
  mux.HandleFunc("/env", func(w http.ResponseWriter, r *http.Request) {
    if os.Getenv(r.URL.Query().Get("name")) != "" {
      w.Write([]byte("1"))
    } else {
      w.Write([]byte("0"))
    }
  })
  ln, err := net.Listen("tcp", "127.0.0.1:"+port)
  if err != nil {
    fmt.Fprintln(os.Stderr, "helper: listen:", err)
    os.Exit(1)
  }
  http.Serve(ln, mux)
}

func fakeLlamaServerBinary(t *testing.T) string {
  t.Helper()

  testBin, err := os.Executable()
  if err != nil {
    t.Fatal(err)
  }

  script := fmt.Sprintf(
    "#!/bin/sh\nexport GO_WANT_HELPER_PROCESS=1\nexec %q -test.run=^TestHelperProcess$ -- \"$@\"\n",
    testBin,
  )
  path := filepath.Join(t.TempDir(), "fake-llama-server")
  if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
    t.Fatal(err)
  }
  return path
}

func freePort(t *testing.T) int {
  t.Helper()
  ln, err := net.Listen("tcp", "127.0.0.1:0")
  if err != nil {
    t.Fatal(err)
  }
  defer ln.Close()
  return ln.Addr().(*net.TCPAddr).Port
}

func TestSpawnHealthPollAndShutdown(t *testing.T) {
  if testing.Short() {
    t.Skip("spawns a real subprocess")
  }

  bin := fakeLlamaServerBinary(t)
  port := freePort(t)

  m := New(map[string]ModelSpec{
    "fim": { Binary: bin, ModelPath: "unused.gguf", Port: port },
  })

  ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
  defer cancel()

  client, err := m.Client(ctx, "fim")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  if err := client.Health(ctx); err != nil {
    t.Fatalf("Health after spawn: %v", err)
  }

  names := m.Names()
  var running bool
  for _, s := range names {
    if s.Name == "fim" {
      running = s.Running
    }
  }
  if !running {
    t.Fatal("Names() should report fim as running after Client spawned it")
  }

  // A second Client call must reuse the already-running process,
  // not spawn a duplicate.
  m.mu.Lock()
  procBefore := m.procs["fim"].cmd.Process.Pid
  m.mu.Unlock()

  if _, err := m.Client(ctx, "fim"); err != nil {
    t.Fatalf("second Client: %v", err)
  }
  m.mu.Lock()
  procAfter := m.procs["fim"].cmd.Process.Pid
  m.mu.Unlock()
  if procBefore != procAfter {
    t.Fatal("a second Client call respawned the process instead of reusing it")
  }

  if err := m.Shutdown(context.Background()); err != nil {
    t.Fatalf("Shutdown: %v", err)
  }
  if err := client.Health(context.Background()); err == nil {
    t.Fatal("expected Health to fail once Shutdown stopped the process")
  }
}

func TestSpawnScrubsSecretEnvVars(t *testing.T) {
  if testing.Short() {
    t.Skip("spawns a real subprocess")
  }

  t.Setenv("ARMIN_TEST_SECRET", "should-not-leak-to-children")

  bin := fakeLlamaServerBinary(t)
  port := freePort(t)

  m := New(map[string]ModelSpec{
    // "claude" is never used in this test — just configured, so its
    // APIKeyEnv name lands in scrubEnvNames the same way it would in
    // a real setup with both an authenticated remote model and a
    // local managed one.
    "claude": { URL: "https://api.anthropic.com/v1", APIKeyEnv: "ARMIN_TEST_SECRET" },
    "fim":    { Binary: bin, ModelPath: "unused.gguf", Port: port },
  })
  defer m.Shutdown(context.Background())

  ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
  defer cancel()

  if _, err := m.Client(ctx, "fim"); err != nil {
    t.Fatalf("Client: %v", err)
  }

  resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/env?name=ARMIN_TEST_SECRET", port))
  if err != nil {
    t.Fatalf("querying the spawned process: %v", err)
  }
  defer resp.Body.Close()
  body, err := io.ReadAll(resp.Body)
  if err != nil {
    t.Fatal(err)
  }
  if string(body) != "0" {
    t.Fatalf(
      "spawned process saw ARMIN_TEST_SECRET (body=%q) — a local "+
        "llama-server must never inherit another model's API key",
      body,
    )
  }
}

func TestSwitchMutatesClientInPlace(t *testing.T) {
  if testing.Short() {
    t.Skip("spawns a real subprocess")
  }

  bin := fakeLlamaServerBinary(t)
  port := freePort(t)

  m := New(map[string]ModelSpec{
    "fim": { Binary: bin, ModelPath: "unused.gguf", Port: port },
  })
  defer m.Shutdown(context.Background())

  ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
  defer cancel()

  client, err := m.Client(ctx, "fim")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  original := client

  if err := m.Switch(ctx, client, "fim"); err != nil {
    t.Fatalf("Switch: %v", err)
  }
  if client != original {
    t.Fatal("Switch must mutate the given *Client, not return a new one")
  }
  if client.Model != "fim" {
    t.Fatalf("Model = %q, want %q", client.Model, "fim")
  }
}
