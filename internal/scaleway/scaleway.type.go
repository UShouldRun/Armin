package scaleway

import (
  "net/http"
)

// Client calls the Instance API in one zone (e.g. fr-par-2).
type Client struct {
  Zone, SecretKey string
  HTTP            *http.Client
  Endpoint        string // tests override; default DefaultEndpoint
}

// Server is what GetServer reports.
type Server struct {
  ID, State           string
  PublicIP, PrivateIP string
}

type ipBody struct {
  Address string `json:"address"`
}

type serverBody struct {
  Server struct {
    ID        string    `json:"id"`
    State     string    `json:"state"`
    PublicIP  *ipBody   `json:"public_ip"`
    PublicIPs []*ipBody `json:"public_ips"`
    PrivateIP *string   `json:"private_ip"`
  } `json:"server"`
}
