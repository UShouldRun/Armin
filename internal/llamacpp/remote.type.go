package llamacpp

import (
  "context"

  "armin/internal/aws"
  "armin/internal/scaleway"
  "armin/internal/stackit"
)

// Provider names the cloud a remote model's machine lives in.
type Provider uint8

// RemoteSpec locates the machine a model runs on and how to reach
// it. Credentials come from the environment (aws.EnvCredentials,
// stackit.EnvTokenSource), never from config.conf.
type RemoteSpec struct {
  Provider  Provider
  MachineID string // EC2 instance, STACKIT or Scaleway server id
  Region    string // AWS region, STACKIT region (eu01), Scaleway zone
  ProjectID string // STACKIT only

  SSHUser, SSHKey string
  RemotePort      int

  // StopOnDisconnect makes the remote side shut the machine down
  // when the SSH session drops (armin killed, laptop asleep, network
  // gone) — a dead man's switch, so a crash never leaves a GPU box
  // billing. Only armed when armin itself started the machine.
  StopOnDisconnect bool
}

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

type ec2Machine struct {
  ec2 *aws.EC2
  id  string
}

type stackitMachine struct {
  client *stackit.Client
  id     string
}

type scalewayMachine struct {
  client *scaleway.Client
  id     string
}
