// The types of scaleway.go.

package cloud

import (
  "net/http"
)

// scalewayClient calls the Instance API in one zone (e.g. fr-par-2).
type scalewayClient struct {
  Zone, SecretKey string
  HTTP            *http.Client
  Endpoint        string // tests override; default scalewayEndpoint
}

// scalewayServer is what GetServer reports.
type scalewayServer struct {
  ID, State           string
  PublicIP, PrivateIP string
}

type scalewayIPBody struct {
  Address string `json:"address"`
}

type scalewayServerBody struct {
  Server struct {
    ID        string            `json:"id"`
    State     string            `json:"state"`
    PublicIP  *scalewayIPBody   `json:"public_ip"`
    PublicIPs []*scalewayIPBody `json:"public_ips"`
    PrivateIP *string           `json:"private_ip"`
  } `json:"server"`
}
