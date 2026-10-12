package stackit

import (
  "crypto/rsa"
  "net/http"
  "sync"
  "time"
)

// TokenSource hands out bearer tokens: a static one, or short-lived
// ones from the key flow, refreshed before they expire.
type TokenSource struct {
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

// Client calls the IaaS v2 API for one project and region.
type Client struct {
  ProjectID, Region string
  Auth              *TokenSource
  HTTP              *http.Client
  Endpoint          string // tests override; default DefaultIaaSEndpoint
}

// Server is what GetServer reports.
type Server struct {
  ID, Status, PowerStatus string
  PublicIP, PrivateIP     string
}

type serverBody struct {
  ID          string `json:"id"`
  Status      string `json:"status"`
  PowerStatus string `json:"powerStatus"`
  Nics        []struct {
    IPv4     string `json:"ipv4"`
    PublicIP string `json:"publicIp"`
  } `json:"nics"`
}
