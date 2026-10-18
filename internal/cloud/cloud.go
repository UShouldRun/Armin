// Package cloud starts and stops the GPU machines armin runs remote
// models on, in three clouds: AWS EC2, STACKIT and Scaleway. Each
// provider is a hand-rolled slice of its API (aws.go, stackit.go,
// scaleway.go), on the standard library; this file is what the rest
// of armin uses:
//
//   Machine        start-and-wait, stop, id: the same for every cloud
//   New            the Machine for a Where, credentials from the
//                  environment (never from config.conf)
//   CredentialEnv  the variables holding those credentials, which
//                  llamacpp scrubs from what builds and tests see
//
// llamacpp's remote models (llamacpp/remote.go) are the only user: it
// asks for a Machine, waits for it, and runs llama-server on it over
// SSH.
package cloud

import (
  "context"
  "fmt"
  "time"
)

// StatePoll is how often WaitRunning re-reads a machine's state
// while it boots (tests shorten it).
var StatePoll = 3 * time.Second

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

// CredentialEnv lists every environment variable a provider reads
// credentials from.
var CredentialEnv = concat(
  awsCredentialEnv, stackitCredentialEnv, scalewayCredentialEnv,
)

func concat(lists ...[]string) []string {
  var out []string
  for _, l := range lists {
    out = append(out, l...)
  }

  return out
}

// New builds the Machine at w, with the provider's credentials from
// the environment.
func New(w Where) (Machine, error) {
  switch w.Provider {
  case ProviderAWS:
    creds, err := awsEnvCredentials()
    if err != nil {
      return nil, fmt.Errorf("\n\t... in New:\n\t%w", err)
    }

    ec2 := newEC2(w.Region, creds)
    ec2.Endpoint = w.Endpoint

    return ec2Machine{ ec2: ec2, id: w.MachineID }, nil

  case ProviderSTACKIT:
    auth, err := stackitEnvAuth()
    if err != nil {
      return nil, fmt.Errorf("\n\t... in New:\n\t%w", err)
    }

    c := newStackitClient(w.ProjectID, w.Region, auth)
    c.Endpoint = w.Endpoint

    return stackitMachine{ client: c, id: w.MachineID }, nil

  case ProviderScaleway:
    c, err := scalewayEnvClient(w.Region)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in New:\n\t%w", err)
    }

    if w.Endpoint != "" {
      c.Endpoint = w.Endpoint
    }

    return scalewayMachine{ client: c, id: w.MachineID }, nil
  }

  return nil, fmt.Errorf("\n\t... in New:\n\t unknown provider %d", w.Provider)
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

// Stop deallocates: a stopped STACKIT server keeps billing.
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
