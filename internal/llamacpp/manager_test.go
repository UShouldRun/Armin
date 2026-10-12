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
  got := (ModelSpec{}).idleTimeout()
  if got != DefaultIdleTimeout {
    t.Fatalf("zero IdleTimeout = %v, want DefaultIdleTimeout", got)
  }
  got = (ModelSpec{ IdleTimeout: 5 * time.Minute }).idleTimeout()
  if got != 5 * time.Minute {
    t.Fatalf("idleTimeout() = %v, want the override", got)
  }
  got = (ModelSpec{ IdleTimeout: -1 }).idleTimeout()
  if got >= 0 {
    t.Fatalf("idleTimeout() = %v, want a negative sentinel preserved", got)
  }
}

func TestModelSpecURL(t *testing.T) {
  got := (ModelSpec{ URL: "http://elsewhere:9000" }).url()
  if got != "http://elsewhere:9000" {
    t.Fatalf("url() = %q, want the external URL unchanged", got)
  }
  got = (ModelSpec{ Binary: "llama-server", Port: 8081 }).url()
  if got != "http://127.0.0.1:8081" {
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

  _, err := m.Client(context.Background(), "nope")
  if err == nil {
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

  err = m.Switch(context.Background(), client, "claude")
  if err != nil {
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
    if a == "--port" && i + 1 < len(os.Args) {
      port = os.Args[i + 1]
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
  ln, err := net.Listen("tcp", "127.0.0.1:" + port)
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
  err = os.WriteFile(path, []byte(script), 0o755)
  if err != nil {
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

  ctx, cancel := context.WithTimeout(context.Background(), 15 * time.Second)
  defer cancel()

  client, err := m.Client(ctx, "fim")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  err = client.Health(ctx)
  if err != nil {
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

  _, err = m.Client(ctx, "fim")
  if err != nil {
    t.Fatalf("second Client: %v", err)
  }
  m.mu.Lock()
  procAfter := m.procs["fim"].cmd.Process.Pid
  m.mu.Unlock()
  if procBefore != procAfter {
    t.Fatal("a second Client call respawned the process instead of reusing it")
  }

  err = m.Shutdown(context.Background())
  if err != nil {
    t.Fatalf("Shutdown: %v", err)
  }
  err = client.Health(context.Background())
  if err == nil {
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

  ctx, cancel := context.WithTimeout(context.Background(), 15 * time.Second)
  defer cancel()

  _, err := m.Client(ctx, "fim")
  if err != nil {
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

  ctx, cancel := context.WithTimeout(context.Background(), 15 * time.Second)
  defer cancel()

  client, err := m.Client(ctx, "fim")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }
  original := client

  err = m.Switch(ctx, client, "fim")
  if err != nil {
    t.Fatalf("Switch: %v", err)
  }
  if client != original {
    t.Fatal("Switch must mutate the given *Client, not return a new one")
  }
  if client.Model != "fim" {
    t.Fatalf("Model = %q, want %q", client.Model, "fim")
  }
}

func TestReadyRespawnsAStoppedModel(t *testing.T) {
  if testing.Short() {
    t.Skip("spawns a real subprocess")
  }

  m := New(map[string]ModelSpec{
    "chat": { Binary: fakeLlamaServerBinary(t), ModelPath: "unused.gguf", Port: freePort(t) },
  })
  defer m.Shutdown(context.Background())

  ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Second)
  defer cancel()

  client, err := m.Client(ctx, "chat")
  if err != nil {
    t.Fatalf("Client: %v", err)
  }

  // What the idle reaper does after idle_timeout.
  m.stopModel(ctx, "chat")
  err = client.Health(ctx)
  if err == nil {
    t.Fatal("the model should be down after being stopped")
  }

  if client.Ready == nil {
    t.Fatal("managed clients must carry a Ready hook")
  }
  err = client.Ready(ctx)
  if err != nil {
    t.Fatalf("Ready: %v", err)
  }
  err = client.Health(ctx)
  if err != nil {
    t.Fatalf("Ready didn't bring the model back: %v", err)
  }
}

func TestSwitchFreesTheGPUFromThePreviousLocalModel(t *testing.T) {
  if testing.Short() {
    t.Skip("spawns real subprocesses")
  }

  bin := fakeLlamaServerBinary(t)
  m := New(map[string]ModelSpec{
    "a":    { Binary: bin, ModelPath: "a.gguf", Port: freePort(t) },
    "b":    { Binary: bin, ModelPath: "b.gguf", Port: freePort(t) },
    "fim":  { Binary: bin, ModelPath: "f.gguf", Port: freePort(t) },
    "api":  { URL: "http://127.0.0.1:1" },
  })
  defer m.Shutdown(context.Background())

  ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Second)
  defer cancel()

  chat, err := m.Client(ctx, "a")
  if err != nil {
    t.Fatalf("Client a: %v", err)
  }
  _, err = m.Client(ctx, "fim")
  if err != nil {
    t.Fatalf("Client fim: %v", err)
  }

  err = m.Switch(ctx, chat, "b")
  if err != nil {
    t.Fatalf("Switch: %v", err)
  }

  m.mu.Lock()
  _, aUp := m.procs["a"]
  _, bUp := m.procs["b"]
  _, fimUp := m.procs["fim"]
  m.mu.Unlock()
  if aUp || !bUp || !fimUp {
    t.Fatalf("a=%v b=%v fim=%v; want a stopped, b and the completion model running", aUp, bUp, fimUp)
  }

  // An external model isn't armin's to stop; switching to it leaves b alone.
  err = m.Switch(ctx, chat, "api")
  if err != nil {
    t.Fatalf("Switch to external: %v", err)
  }
  m.mu.Lock()
  _, bUp = m.procs["b"]
  m.mu.Unlock()
  if !bUp {
    t.Fatal("switching to an external model stopped a local one")
  }
}

// TestSwitchChatKeepsCompletionLocal: completion follows chat between
// local models (one on the GPU at a time) and stays on the last
// local one while chat uses a cloud model.
func TestSwitchChatKeepsCompletionLocal(t *testing.T) {
  if testing.Short() {
    t.Skip("spawns real subprocesses")
  }

  bin := fakeLlamaServerBinary(t)
  m := New(map[string]ModelSpec{
    "a":     { Binary: bin, ModelPath: "a.gguf", Port: freePort(t) },
    "b":     { Binary: bin, ModelPath: "b.gguf", Port: freePort(t) },
    "cloud": { URL: "http://127.0.0.1:1" },
  })
  defer m.Shutdown(context.Background())

  ctx, cancel := context.WithTimeout(context.Background(), 20 * time.Second)
  defer cancel()

  chat, err := m.Client(ctx, "a")
  if err != nil {
    t.Fatalf("Client a: %v", err)
  }

  completion, err := m.LazyClient("a")
  if err != nil {
    t.Fatal(err)
  }

  up := func(name string) bool {
    m.mu.Lock()
    defer m.mu.Unlock()
    _, ok := m.procs[name]

    return ok
  }

  err = m.SwitchChat(ctx, chat, completion, "cloud")
  if err != nil {
    t.Fatalf("to cloud: %v", err)
  }

  if chat.Model != "cloud" || completion.Model != "a" || !up("a") {
    t.Fatalf("chat %q, completion %q, a up %v; want completion kept on a", chat.Model, completion.Model, up("a"))
  }

  err = m.SwitchChat(ctx, chat, completion, "b")
  if err != nil {
    t.Fatalf("to b: %v", err)
  }

  if chat.Model != "b" || completion.Model != "b" || up("a") || !up("b") {
    t.Fatalf("chat %q, completion %q, a up %v, b up %v; want both on b, a stopped",
      chat.Model, completion.Model, up("a"), up("b"))
  }

  err = m.SwitchChat(ctx, chat, nil, "a")
  if err != nil {
    t.Fatalf("pinned: %v", err)
  }

  if completion.Model != "b" {
    t.Fatalf("a pinned completion moved to %q", completion.Model)
  }
}

// TestSwitchToHostedIDOnTheFly: "base:id" reuses a hosted model's
// endpoint and key with another id, no config block needed.
func TestSwitchToHostedIDOnTheFly(t *testing.T) {
  m := New(map[string]ModelSpec{
    "planner": { URL: "http://127.0.0.1:1/api/v1", ID: "qwen/qwen3.8-27b", Effort: "medium" },
    "local":   { Binary: "llama-server", ModelPath: "a.gguf", Port: 1 },
  })

  if m.Has("local:x") || m.Has("nope:x") {
    t.Fatal("only a hosted base can be reused")
  }

  if !m.Has("planner:nvidia/nemotron-3-ultra-550b-a55b:free") {
    t.Fatal("planner:<id> not adopted")
  }

  chat := New(map[string]ModelSpec{}).newClientFor("x", "http://127.0.0.1:1")
  err := m.Switch(context.Background(), chat, "planner:deepseek/deepseek-v4-pro-0813")
  if err != nil {
    t.Fatal(err)
  }

  if chat.ID != "deepseek/deepseek-v4-pro-0813" || chat.Effort != "medium" || chat.BaseURL != "http://127.0.0.1:1/api/v1" {
    t.Fatalf("client = %q %q %q", chat.ID, chat.Effort, chat.BaseURL)
  }

  if m.Label("planner:deepseek/deepseek-v4-pro-0813") != "deepseek/deepseek-v4-pro-0813" {
    t.Fatalf("label = %q", m.Label("planner:deepseek/deepseek-v4-pro-0813"))
  }
}
