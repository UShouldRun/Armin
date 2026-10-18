// Running a llama-server: spawn starts one (here or, through
// remote.go, on a cloud machine) and waits until it's healthy;
// process tracks it and keeps the end of its output for errors; stop
// ends it. Children get an environment without API keys or cloud
// credentials (scrubbedEnviron).

package llamacpp

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "strings"
  "syscall"
  "time"

  "armin/internal/llm"
)

// exited reports whether the process has already died.
func (p *process) exited() bool {
  if p.done == nil {
    return false
  }

  select {
  case <-p.done:
    return true
  default:
    return false
  }
}

// startProcess starts cmd with one goroutine owning cmd.Wait.
func startProcess(cmd *exec.Cmd) (*process, error) {
  out := &tailBuffer{ max: 4096 }
  cmd.Stdout, cmd.Stderr = out, out
  dieWithParent(cmd)
  err := cmd.Start()
  if err != nil {
    return nil, err
  }

  p := &process{ cmd: cmd, done: make(chan struct{}), out: out }
  go func() {
    cmd.Wait()
    close(p.done)
  }()

  return p, nil
}

func (t *tailBuffer) Write(b []byte) (int, error) {
  t.mu.Lock()
  defer t.mu.Unlock()
  t.buf = append(t.buf, b...)
  over := len(t.buf) - t.max
  if over > 0 {
    t.buf = append(t.buf[:0], t.buf[over:]...)
  }

  return len(b), nil
}

func (t *tailBuffer) String() string {
  if t == nil {
    return ""
  }

  t.mu.Lock()
  defer t.mu.Unlock()

  return strings.TrimSpace(string(t.buf))
}

func (m *Manager) spawn(ctx context.Context, name string, spec ModelSpec) error {
  m.mu.Lock()
  spec = spec.inTier(m.tiers[name])
  m.mu.Unlock()

  if spec.Remote != nil {
    return m.spawnRemote(ctx, name, spec)
  }

  args := append(
    []string{ "-m", spec.ModelPath, "--port", fmt.Sprint(spec.Port) },
    serverArgs(spec)...,
  )
  cmd := exec.Command(spec.Binary, args...)
  // Independent of ctx on purpose: this process must outlive
  // whatever single request triggered spawning it. Its lifetime is
  // owned entirely by the reaper and Shutdown below.
  cmd.Env = m.scrubbedEnviron()
  proc, err := startProcess(cmd)
  if err != nil {
    return fmt.Errorf(
      "\n\t... in spawn:\n\t starting %q for %q: %w",
      spec.Binary, name, err,
    )
  }

  proc.lastUsed = time.Now()

  m.mu.Lock()
  m.procs[name] = proc
  m.mu.Unlock()

  err = waitHealthy(ctx, spec.url(), proc, spawnHealthTimeout)
  if err != nil {
    m.mu.Lock()
    delete(m.procs, name)
    m.mu.Unlock()
    proc.stop(ctx)

    return fmt.Errorf(
      "\n\t... in spawn:\n\t %q never became healthy: %w\n\t llama-server " +
        "said: %s",
      name, err, lastLines(proc.out.String(), 4),
    )
  }

  return nil
}

// scrubbedEnviron is os.Environ() with every configured
// models.<name>.api_key_env name removed. exec.Cmd with a nil Env
// inherits the parent's environment in full, which would otherwise
// hand a Claude (or any other remote model's) API key to a local
// llama-server process that has no business seeing it.
// Environ is armin's environment without any API key or cloud
// credential, for every other process armin starts (builds, tests,
// programs the model runs).
func (m *Manager) Environ() []string {
  return m.scrubbedEnviron()
}

func (m *Manager) scrubbedEnviron() []string {
  full := os.Environ()
  out := make([]string, 0, len(full))
  for _, kv := range full {
    name, _, _ := strings.Cut(kv, "=")
    if m.scrubEnvNames[name] {
      continue
    }

    out = append(out, kv)
  }

  return out
}

// waitHealthy polls url until llama-server answers, giving up early
// if the process behind it dies.
func waitHealthy(
  ctx context.Context, url string, proc *process, timeout time.Duration,
) error {
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()

  client := llm.New(url, "")
  for {
    err := client.Health(ctx)
    if err == nil {
      return nil
    }

    select {
    case <-ctx.Done():
      return fmt.Errorf("timed out waiting for llama-server to come up")
    case <-proc.done:
      return fmt.Errorf("the process exited before llama-server came up")
    case <-time.After(healthPoll):
    }
  }
}

// stop sends SIGTERM and waits briefly for a clean exit, falling
// back to SIGKILL; then stops the EC2 instance if armin started it.
func (p *process) stop(ctx context.Context) {
  if p.cmd.Process != nil && p.done != nil {
    p.cmd.Process.Signal(syscall.SIGTERM)
    select {
    case <-p.done:
    case <-time.After(stopGraceTimeout):
      p.cmd.Process.Kill()
      <-p.done
    }
  }

  if p.remote != nil && p.remote.started {
    p.remote.stopMachine()
  }
}

// stopMachine gets its own deadline: it must run even when the
// caller's context is the one being cancelled.
func (r *remoteRun) stopMachine() {
  ctx, cancel := context.WithTimeout(context.Background(), 30 * time.Second)
  defer cancel()
  r.machine.Stop(ctx)
}
