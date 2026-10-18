// The types of stackit.go.

package cloud

import (
  "crypto/rsa"
  "net/http"
  "sync"
  "time"
)

// stackitAuth hands out bearer tokens: a static one, or short-lived
// ones from the key flow, refreshed before they expire.
type stackitAuth struct {
  static string

  creds    keyCredentials
  key      *rsa.PrivateKey
  tokenURL string
  http     *http.Client

  mu      sync.Mutex
  token   string
  expires time.Time
}

type keyCredentials struct {
  Aud           string `json:"aud"`
  Iss           string `json:"iss"`
  Kid           string `json:"kid"`
  Sub           string `json:"sub"`
  PrivateKey    string `json:"privateKey"`
  TokenEndpoint string `json:"tokenEndpoint"`
}

type keyFile struct {
  Credentials *keyCredentials `json:"credentials"`
}

// stackitClient calls the IaaS v2 API for one project and region.
type stackitClient struct {
  ProjectID, Region string
  Auth              *stackitAuth
  HTTP              *http.Client
  Endpoint          string // tests override; default DefaultIaaSEndpoint
}

// stackitServer is what GetServer reports.
type stackitServer struct {
  ID, Status, PowerStatus string
  PublicIP, PrivateIP     string
}

type stackitServerBody struct {
  ID          string `json:"id"`
  Status      string `json:"status"`
  PowerStatus string `json:"powerStatus"`
  Nics        []struct {
    IPv4     string `json:"ipv4"`
    PublicIP string `json:"publicIp"`
  } `json:"nics"`
}
