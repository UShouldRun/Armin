// Where a remote model's machine is and how to reach it.

package llamacpp

import (
  "armin/internal/cloud"
)

// RemoteSpec locates the machine a model runs on and how to reach
// it. Credentials come from the environment (see cloud.New), never
// from config.conf.
type RemoteSpec struct {
  Provider  cloud.Provider
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
