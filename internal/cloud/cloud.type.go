// The types of cloud.go.

package cloud

import (
  "context"
)

// Provider names the cloud a machine lives in.
type Provider uint8

// Machine is one cloud machine armin can start and stop.
type Machine interface {
  ID() string
  // WaitRunning starts the machine if it's off and waits until it's
  // reachable; started reports whether this call started it.
  WaitRunning(
    ctx context.Context, status func(string),
  ) (host string, started bool, err error)
  // Stop ends compute billing for the machine.
  Stop(ctx context.Context) error
}

// Where is one machine: its provider, id and location, and an API
// endpoint override for tests.
type Where struct {
  Provider  Provider
  MachineID string // EC2 instance, STACKIT or Scaleway server id
  Region    string // AWS region, STACKIT region (eu01), Scaleway zone
  ProjectID string // STACKIT only
  Endpoint  string // empty: the provider's real API
}

type ec2Machine struct {
  ec2 *ec2Client
  id  string
}

type stackitMachine struct {
  client *stackitClient
  id     string
}

type scalewayMachine struct {
  client *scalewayClient
  id     string
}
