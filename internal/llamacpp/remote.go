package llamacpp

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/aws"
  "armin/internal/scaleway"
  "armin/internal/stackit"
)

const (
  ProviderAWS      Provider = 1 << iota // EC2: start / stop
  ProviderSTACKIT                       // IaaS v2: start / deallocate
  ProviderScaleway                      // Instance: poweron / poweroff
  ProviderAll      = ProviderAWS | ProviderSTACKIT | ProviderScaleway
)

func (p Provider) String() string {
  switch p {
  case ProviderAWS:
    return "EC2 instance"
  case ProviderSTACKIT:
    return "STACKIT server"
  case ProviderScaleway:
    return "Scaleway instance"
  }

  return "machine"
}

func (e ec2Machine) ID() string { return e.id }

func (e ec2Machine) WaitRunning(
  ctx context.Context, status func(string),
) (string, bool, error) {
  inst, started, err := e.ec2.WaitRunning(ctx, e.id, status)
  return inst.Host(), started, err
}

func (e ec2Machine) Stop(ctx context.Context) error {
  return e.ec2.StopInstance(ctx, e.id)
}

func (s stackitMachine) ID() string { return s.id }

func (s stackitMachine) WaitRunning(
  ctx context.Context, status func(string),
) (string, bool, error) {
  srv, started, err := s.client.WaitRunning(ctx, s.id, status)
  return srv.Host(), started, err
}

// Stop deallocates: a merely stopped STACKIT server keeps billing.
func (s stackitMachine) Stop(ctx context.Context) error {
  return s.client.DeallocateServer(ctx, s.id)
}

func (s scalewayMachine) ID() string { return s.id }

func (s scalewayMachine) WaitRunning(
  ctx context.Context, status func(string),
) (string, bool, error) {
  srv, started, err := s.client.WaitRunning(ctx, s.id, status)
  return srv.Host(), started, err
}

// Stop powers off: stop_in_place would keep the slot reserved.
func (s scalewayMachine) Stop(ctx context.Context) error {
  return s.client.PowerOff(ctx, s.id)
}

// machineFor builds the provider client from environment credentials.
func (m *Manager) machineFor(r *RemoteSpec) (Machine, error) {
  switch r.Provider {
  case ProviderAWS:
    creds, err := aws.EnvCredentials()
    if err != nil {
      return nil, err
    }

    ec2 := aws.NewEC2(r.Region, creds)
    ec2.Endpoint = m.EC2Endpoint

    return ec2Machine{ ec2: ec2, id: r.MachineID }, nil

  case ProviderSTACKIT:
    auth, err := stackit.EnvTokenSource()
    if err != nil {
      return nil, err
    }

    c := stackit.NewClient(r.ProjectID, r.Region, auth)
    c.Endpoint = m.StackitEndpoint

    return stackitMachine{ client: c, id: r.MachineID }, nil

  case ProviderScaleway:
    c, err := scaleway.EnvClient(r.Region)
    if err != nil {
      return nil, err
    }

    if m.ScalewayEndpoint != "" {
      c.Endpoint = m.ScalewayEndpoint
    }

    return scalewayMachine{ client: c, id: r.MachineID }, nil
  }

  return nil, fmt.Errorf("unknown provider %d", r.Provider)
}

const (
  // remoteStartTimeout covers a boot plus loading a large model from
  // a cold volume.
  remoteStartTimeout = 20 * time.Minute
  // sshRetryDelay paces reconnects while sshd is still booting.
  sshRetryDelay = 3 * time.Second
)

func (m *Manager) status(format string, args ...any) {
  if m.OnStatus != nil {
    m.OnStatus(fmt.Sprintf(format, args...))
  }
}

// spawnRemote starts the machine if needed, then runs llama-server
// on it through an SSH session that also forwards spec.Port to it.
// Every failure path stops a machine this call started.
func (m *Manager) spawnRemote(
  ctx context.Context, name string, spec ModelSpec,
) error {
  r := spec.Remote
  machine, err := m.machineFor(r)
  if err != nil {
    return fmt.Errorf("\n\t... in spawnRemote:\n\t%w", err)
  }

  ctx, cancel := context.WithTimeout(ctx, remoteStartTimeout)
  defer cancel()

  m.status("%s: checking %s %s", name, r.Provider, r.MachineID)
  host, started, err := machine.WaitRunning(
    ctx, func(s string) { m.status("%s: %s", name, s) },
  )

  m.mu.Lock()
  started = started || m.owned[r.MachineID] != nil
  delete(m.owned, r.MachineID)
  m.mu.Unlock()

  run := &remoteRun{ machine: machine, started: started }
  fail := func(err error) error {
    if run.started {
      m.status("%s: stopping %s %s", name, r.Provider, r.MachineID)
      run.stopMachine()
    }

    return fmt.Errorf("\n\t... in spawnRemote:\n\t%w", err)
  }

  if err != nil {
    return fail(err)
  }

  for attempt := 1; ; attempt++ {
    m.status(
      "%s: connecting to %s and loading the model (attempt %d)",
      name, host, attempt,
    )
    cmd := exec.Command(
      m.sshBinary(), sshArgs(spec, host, started, m.KnownHostsFile)...,
    )
    cmd.Env = m.scrubbedEnviron()
    proc, err := startProcess(cmd)
    if err != nil {
      return fail(fmt.Errorf("starting ssh: %w", err))
    }

    proc.remote, proc.lastUsed = run, time.Now()

    err = waitHealthy(ctx, spec.url(), proc, remoteStartTimeout)
    if err == nil {
      m.mu.Lock()
      m.procs[name] = proc
      m.mu.Unlock()
      m.status("%s: ready on %s", name, spec.url())

      return nil
    }

    proc.remote = nil // the machine outlives this attempt
    proc.stop(ctx)

    out := proc.out.String()
    retry := proc.exited() && ctx.Err() == nil && sshRetryable(out)
    if !retry {
      return fail(fmt.Errorf("%w\n\t ssh output: %s", err, lastLines(out, 8)))
    }

    select {
    case <-ctx.Done():
      return fail(ctx.Err())
    case <-time.After(sshRetryDelay):
    }
  }
}

func (m *Manager) sshBinary() string {
  if m.SSHBinary != "" {
    return m.SSHBinary
  }

  return "ssh"
}

// sshRetryable: sshd isn't up yet (fresh boot) — as opposed to a bad
// key or a remote llama-server that failed to load.
func sshRetryable(out string) bool {
  for _, s := range []string{
    "Connection refused", "Connection timed out", "No route to host",
    "Connection closed", "Connection reset", "Operation timed out",
  } {
    if strings.Contains(out, s) {
      return true
    }
  }

  return false
}

func lastLines(s string, n int) string {
  lines := strings.Split(s, "\n")
  if len(lines) > n {
    lines = lines[len(lines) - n:]
  }

  return strings.Join(lines, "\n\t ")
}

func expandHome(path string) string {
  rest, ok := strings.CutPrefix(path, "~/")
  if ok {
    home, err := os.UserHomeDir()
    if err == nil {
      return filepath.Join(home, rest)
    }
  }

  return path
}

// sshArgs builds the one SSH session that both runs llama-server on
// the machine (bound to its loopback only) and forwards spec.Port
// to it, so nothing is exposed and traffic is encrypted. -tt gives
// the remote side a terminal, so it gets SIGHUP when the session
// drops — which ends llama-server and, with guard, shuts the
// machine down.
func sshArgs(
  spec ModelSpec, host string, guard bool, knownHosts string,
) []string {
  r := spec.Remote
  args := []string{
    "-tt",
    "-o", "BatchMode=yes",
    "-o", "ExitOnForwardFailure=yes",
    "-o", "ServerAliveInterval=15",
    "-o", "ServerAliveCountMax=4",
    "-o", "ConnectTimeout=10",
    "-o", "StrictHostKeyChecking=accept-new",
    "-o", "HostKeyAlias=" + r.MachineID,
  }

  if knownHosts != "" {
    args = append(args, "-o", "UserKnownHostsFile=" + knownHosts)
  }

  if r.SSHKey != "" {
    args = append(args, "-i", expandHome(r.SSHKey))
  }

  args = append(args,
    "-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", spec.Port, r.RemotePort),
    r.SSHUser + "@" + host,
    remoteCommand(spec, guard && r.StopOnDisconnect),
  )

  return args
}

// remoteCommand is the shell line run on the machine.
func remoteCommand(spec ModelSpec, guard bool) string {
  parts := []string{
    shellQuote(spec.Binary),
    "-m", shellQuote(spec.ModelPath),
    "--host", "127.0.0.1",
    "--port", fmt.Sprint(spec.Remote.RemotePort),
  }

  for _, a := range serverArgs(spec) {
    parts = append(parts, shellQuote(a))
  }

  line := strings.Join(parts, " ")
  if !guard {
    return "exec " + line
  }

  return "trap 'sudo -n shutdown -h now' HUP; " + line
}

// shellQuote single-quotes s unless it's plainly safe.
func shellQuote(s string) string {
  safe := s != ""
  for _, r := range s {
    ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
      r >= '0' && r <= '9' || strings.ContainsRune("-_./=:,+@%", r)
    if !ok {
      safe = false
      break
    }
  }

  if safe {
    return s
  }

  return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
