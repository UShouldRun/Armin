// Package stackit is the slice of STACKIT (Schwarz Gruppe's cloud)
// armin needs to run a model on a server it starts and deallocates
// itself: service-account authentication and the IaaS v2 server
// calls (get, start, deallocate). Standard library only.
//
// Deallocate, not stop: a stopped STACKIT server stays on its
// hypervisor and keeps billing full price; a deallocated one bills
// only its volume, and start re-allocates it.
package stackit

import (
  "context"
  "crypto"
  "crypto/rand"
  "crypto/rsa"
  "crypto/sha512"
  "crypto/x509"
  "encoding/base64"
  "encoding/json"
  "encoding/pem"
  "errors"
  "fmt"
  "io"
  "net/http"
  "net/url"
  "os"
  "strings"
  "sync"
  "time"
)

const (
  // DefaultIaaSEndpoint is the global IaaS v2 host; the region is
  // part of the path.
  DefaultIaaSEndpoint = "https://iaas.api.stackit.cloud"
  // defaultTokenURL is the SDK's fallback when the key file names
  // no tokenEndpoint.
  defaultTokenURL = "https://service-account.api.stackit.cloud/token"
  jwtBearerGrant  = "urn:ietf:params:oauth:grant-type:jwt-bearer"
)

// CredentialEnv lists the variables credentials come from — the
// same names the STACKIT SDK and CLI use — and what a Manager must
// keep out of spawned processes.
var CredentialEnv = []string{
  "STACKIT_SERVICE_ACCOUNT_TOKEN",
  "STACKIT_SERVICE_ACCOUNT_KEY",
  "STACKIT_SERVICE_ACCOUNT_KEY_PATH",
  "STACKIT_PRIVATE_KEY",
  "STACKIT_PRIVATE_KEY_PATH",
  "STACKIT_TOKEN_BASEURL",
}

// ErrNoCredentials means no token and no key were configured.
var ErrNoCredentials = errors.New(
  "no STACKIT credentials: set STACKIT_SERVICE_ACCOUNT_KEY_PATH to " +
    "a service account key file (or STACKIT_SERVICE_ACCOUNT_TOKEN), " +
    "e.g. in ~/.config/armin/.env",
)

// --- authentication ---

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

// readEnvOrFile returns the variable's value, or the content of the
// file the _PATH variant names.
func readEnvOrFile(valueEnv, pathEnv string) (string, error) {
  if v := os.Getenv(valueEnv); v != "" {
    return v, nil
  }

  path := os.Getenv(pathEnv)
  if path == "" {
    return "", nil
  }

  if rest, ok := strings.CutPrefix(path, "~/"); ok {
    if home, err := os.UserHomeDir(); err == nil {
      path = home + "/" + rest
    }
  }

  raw, err := os.ReadFile(path)
  if err != nil {
    return "", fmt.Errorf("\n\t... in readEnvOrFile:\n\t %s: %w", pathEnv, err)
  }

  return string(raw), nil
}

// EnvTokenSource builds a TokenSource from the environment: a
// static STACKIT_SERVICE_ACCOUNT_TOKEN wins; otherwise the key flow
// with STACKIT_SERVICE_ACCOUNT_KEY(_PATH), whose private key is
// either inside the key file or in STACKIT_PRIVATE_KEY(_PATH).
func EnvTokenSource() (*TokenSource, error) {
  if t := os.Getenv("STACKIT_SERVICE_ACCOUNT_TOKEN"); t != "" {
    return &TokenSource{ static: t }, nil
  }

  raw, err := readEnvOrFile(
    "STACKIT_SERVICE_ACCOUNT_KEY", "STACKIT_SERVICE_ACCOUNT_KEY_PATH",
  )
  if err != nil {
    return nil, fmt.Errorf("\n\t... in EnvTokenSource:\n\t%w", err)
  }

  if raw == "" {
    return nil, ErrNoCredentials
  }

  var kf keyFile
  if err := json.Unmarshal([]byte(raw), &kf); err != nil || kf.Credentials == nil {
    return nil, fmt.Errorf(
      "\n\t... in EnvTokenSource:\n\t the service account key isn't a " +
        "STACKIT key file (no \"credentials\" object)",
    )
  }

  pemText := kf.Credentials.PrivateKey
  if pemText == "" {
    pemText, err = readEnvOrFile("STACKIT_PRIVATE_KEY", "STACKIT_PRIVATE_KEY_PATH")
    if err != nil {
      return nil, fmt.Errorf("\n\t... in EnvTokenSource:\n\t%w", err)
    }
  }

  if pemText == "" {
    return nil, fmt.Errorf(
      "\n\t... in EnvTokenSource:\n\t the key file has no privateKey; " +
        "set STACKIT_PRIVATE_KEY_PATH to the matching private key",
    )
  }

  key, err := parseRSAKey(pemText)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in EnvTokenSource:\n\t%w", err)
  }

  tokenURL := os.Getenv("STACKIT_TOKEN_BASEURL")
  if tokenURL == "" {
    tokenURL = kf.Credentials.TokenEndpoint
  }

  if tokenURL == "" {
    tokenURL = defaultTokenURL
  }

  return &TokenSource{
    creds:    *kf.Credentials,
    key:      key,
    tokenURL: tokenURL,
    http:     &http.Client{ Timeout: 30 * time.Second },
  }, nil
}

// parseRSAKey accepts PKCS#8 or PKCS#1 PEM, including the escaped
// "\n" form some key files store.
func parseRSAKey(text string) (*rsa.PrivateKey, error) {
  text = strings.ReplaceAll(text, `\n`, "\n")
  block, _ := pem.Decode([]byte(text))
  if block == nil {
    return nil, fmt.Errorf("private key is not PEM")
  }

  if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
    rk, ok := k.(*rsa.PrivateKey)
    if !ok {
      return nil, fmt.Errorf("private key is not RSA")
    }

    return rk, nil
  }

  k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
  if err != nil {
    return nil, fmt.Errorf("parsing private key: %w", err)
  }

  return k, nil
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

// uuid4 is a random RFC 4122 v4 UUID (the jti claim).
func uuid4() string {
  var b [16]byte
  rand.Read(b[:])
  b[6] = b[6] & 0x0f | 0x40
  b[8] = b[8] & 0x3f | 0x80

  return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// assertion is the RS512 self-signed JWT the key flow exchanges.
func (t *TokenSource) assertion(now time.Time) (string, error) {
  header, _ := json.Marshal(map[string]string{
    "alg": "RS512", "typ": "JWT", "kid": t.creds.Kid,
  })
  claims, _ := json.Marshal(map[string]any{
    "iss": t.creds.Iss,
    "sub": t.creds.Sub,
    "aud": t.creds.Aud,
    "jti": uuid4(),
    "iat": now.Unix(),
    "exp": now.Add(10 * time.Minute).Unix(),
  })

  signing := b64(header) + "." + b64(claims)
  sum := sha512.Sum512([]byte(signing))
  sig, err := rsa.SignPKCS1v15(rand.Reader, t.key, crypto.SHA512, sum[:])
  if err != nil {
    return "", fmt.Errorf("\n\t... in assertion:\n\t%w", err)
  }

  return signing + "." + b64(sig), nil
}

// tokenSlack refreshes a cached token this long before it expires.
const tokenSlack = time.Minute

// Token returns a bearer token, exchanging a fresh assertion only
// when the cached one is about to expire.
func (t *TokenSource) Token(ctx context.Context) (string, error) {
  if t.static != "" {
    return t.static, nil
  }

  t.mu.Lock()
  defer t.mu.Unlock()
  if t.token != "" && time.Now().Add(tokenSlack).Before(t.expires) {
    return t.token, nil
  }

  jwt, err := t.assertion(time.Now())
  if err != nil {
    return "", err
  }

  form := url.Values{ "grant_type": { jwtBearerGrant }, "assertion": { jwt } }
  req, err := http.NewRequestWithContext(
    ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()),
  )
  if err != nil {
    return "", fmt.Errorf("\n\t... in Token:\n\t%w", err)
  }

  req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

  resp, err := t.http.Do(req)
  if err != nil {
    return "", fmt.Errorf("\n\t... in Token:\n\t%w", err)
  }

  defer resp.Body.Close()
  raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64 << 10))
  if resp.StatusCode != http.StatusOK {
    return "", fmt.Errorf(
      "\n\t... in Token:\n\t token endpoint returned %d: %s",
      resp.StatusCode, strings.TrimSpace(string(raw)),
    )
  }

  var body struct {
    AccessToken string `json:"access_token"`
    ExpiresIn   int64  `json:"expires_in"`
  }

  if err := json.Unmarshal(raw, &body); err != nil || body.AccessToken == "" {
    return "", fmt.Errorf("\n\t... in Token:\n\t no access_token in response")
  }

  if body.ExpiresIn <= 0 {
    body.ExpiresIn = 600
  }

  t.token = body.AccessToken
  t.expires = time.Now().Add(time.Duration(body.ExpiresIn) * time.Second)

  return t.token, nil
}

// --- IaaS v2 servers ---

// Client calls the IaaS v2 API for one project and region.
type Client struct {
  ProjectID, Region string
  Auth              *TokenSource
  HTTP              *http.Client
  Endpoint          string // tests override; default DefaultIaaSEndpoint
}

// NewClient returns a client for project and region (e.g. "eu01").
func NewClient(projectID, region string, auth *TokenSource) *Client {
  return &Client{
    ProjectID: projectID,
    Region:    region,
    Auth:      auth,
    HTTP:      &http.Client{ Timeout: 30 * time.Second },
  }
}

// Server is what GetServer reports.
type Server struct {
  ID, Status, PowerStatus string
  PublicIP, PrivateIP     string
}

// Host is the address to reach the server at: a public IP, else
// the first private one (over a VPN).
func (s Server) Host() string {
  if s.PublicIP != "" {
    return s.PublicIP
  }

  return s.PrivateIP
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

func (c *Client) serverURL(id, action string) string {
  endpoint := c.Endpoint
  if endpoint == "" {
    endpoint = DefaultIaaSEndpoint
  }

  u := fmt.Sprintf(
    "%s/v2/projects/%s/regions/%s/servers/%s",
    endpoint, url.PathEscape(c.ProjectID),
    url.PathEscape(c.Region), url.PathEscape(id),
  )
  if action != "" {
    u += "/" + action
  }

  return u
}

func (c *Client) do(
  ctx context.Context, method, u string, out any,
) error {
  token, err := c.Auth.Token(ctx)
  if err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  req, err := http.NewRequestWithContext(ctx, method, u, nil)
  if err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  req.Header.Set("Authorization", "Bearer " + token)
  req.Header.Set("Accept", "application/json")

  resp, err := c.HTTP.Do(req)
  if err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  defer resp.Body.Close()
  raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1 << 20))

  if resp.StatusCode >= 300 {
    var e struct {
      Msg string `json:"msg"`
    }

    msg := strings.TrimSpace(string(raw))
    if json.Unmarshal(raw, &e) == nil && e.Msg != "" {
      msg = e.Msg
    }

    return fmt.Errorf(
      "\n\t... in do:\n\t %s %s: HTTP %d: %s",
      method, u, resp.StatusCode, msg,
    )
  }

  if out == nil {
    return nil
  }

  if err := json.Unmarshal(raw, out); err != nil {
    return fmt.Errorf("\n\t... in do:\n\t%w", err)
  }

  return nil
}

// GetServer reports one server's status and addresses.
func (c *Client) GetServer(ctx context.Context, id string) (Server, error) {
  var b serverBody
  err := c.do(ctx, http.MethodGet, c.serverURL(id, "") + "?details=true", &b)
  if err != nil {
    return Server{}, fmt.Errorf("\n\t... in GetServer:\n\t%w", err)
  }

  s := Server{ ID: b.ID, Status: b.Status, PowerStatus: b.PowerStatus }
  for _, n := range b.Nics {
    if s.PublicIP == "" && n.PublicIP != "" {
      s.PublicIP = n.PublicIP
    }

    if s.PrivateIP == "" && n.IPv4 != "" {
      s.PrivateIP = n.IPv4
    }
  }

  return s, nil
}

// StartServer starts a stopped server, or re-allocates a
// deallocated one. It returns before the server is running.
func (c *Client) StartServer(ctx context.Context, id string) error {
  if err := c.do(ctx, http.MethodPost, c.serverURL(id, "start"), nil); err != nil {
    return fmt.Errorf("\n\t... in StartServer:\n\t%w", err)
  }

  return nil
}

// DeallocateServer shuts the server down and releases it from its
// hypervisor, so only the volume keeps billing.
func (c *Client) DeallocateServer(ctx context.Context, id string) error {
  if err := c.do(ctx, http.MethodPost, c.serverURL(id, "deallocate"), nil); err != nil {
    return fmt.Errorf("\n\t... in DeallocateServer:\n\t%w", err)
  }

  return nil
}

// StatePoll is how often WaitRunning re-reads (tests shorten it).
var StatePoll = 3 * time.Second

// WaitRunning polls until the server is ACTIVE with an address,
// starting it if it's INACTIVE or DEALLOCATED. started reports
// whether this call started it.
func (c *Client) WaitRunning(
  ctx context.Context, id string, status func(string),
) (srv Server, started bool, err error) {
  last := ""
  for {
    srv, err = c.GetServer(ctx, id)
    if err != nil {
      return srv, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", err)
    }

    if srv.Status != last && status != nil {
      status(fmt.Sprintf("server %s is %s", id, srv.Status))
    }

    last = srv.Status

    switch srv.Status {
    case "ACTIVE":
      if srv.Host() != "" {
        return srv, started, nil
      }
    case "INACTIVE", "DEALLOCATED":
      if !started {
        if err := c.StartServer(ctx, id); err != nil {
          return srv, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", err)
        }

        started = true
      }
    case "ERROR", "DELETED", "DELETING":
      return srv, started, fmt.Errorf(
        "\n\t... in WaitRunning:\n\t server %s is %s", id, srv.Status,
      )
    }

    select {
    case <-ctx.Done():
      return srv, started, fmt.Errorf("\n\t... in WaitRunning:\n\t%w", ctx.Err())
    case <-time.After(StatePoll):
    }
  }
}
