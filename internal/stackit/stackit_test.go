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
  "fmt"
  "io"
  "net/http"
  "net/http/httptest"
  "net/url"
  "os"
  "path/filepath"
  "strings"
  "sync"
  "testing"
  "time"
)

// writeKeyFile writes a STACKIT-style service account key with an
// embedded PKCS#8 private key (newlines escaped, as STACKIT stores
// them) and returns the key for verification.
func writeKeyFile(t *testing.T, tokenURL string) *rsa.PrivateKey {
  t.Helper()
  key, _ := rsa.GenerateKey(rand.Reader, 2048)
  der, _ := x509.MarshalPKCS8PrivateKey(key)
  pemText := string(pem.EncodeToMemory(&pem.Block{ Type: "PRIVATE KEY", Bytes: der }))

  file := map[string]any{
    "id": "k-1",
    "credentials": map[string]string{
      "aud":           "https://stackit-service-account-prod.apps.01.cf.eu01.stackit.cloud",
      "iss":           "armin@sa.stackit.cloud",
      "kid":           "kid-1",
      "sub":           "11111111-2222-3333-4444-555555555555",
      "privateKey":    strings.ReplaceAll(pemText, "\n", `\n`),
      "tokenEndpoint": tokenURL,
    },
  }

  raw, _ := json.Marshal(file)
  path := filepath.Join(t.TempDir(), "sa.json")
  os.WriteFile(path, raw, 0o600)

  t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "")
  t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", path)

  return key
}

func verifyJWT(t *testing.T, jwt string, pub *rsa.PublicKey) map[string]any {
  t.Helper()
  parts := strings.Split(jwt, ".")
  if len(parts) != 3 {
    t.Fatalf("jwt has %d parts", len(parts))
  }

  var header map[string]string
  raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
  json.Unmarshal(raw, &header)
  if header["alg"] != "RS512" || header["kid"] != "kid-1" || header["typ"] != "JWT" {
    t.Errorf("header = %v", header)
  }

  sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
  sum := sha512.Sum512([]byte(parts[0] + "." + parts[1]))
  err := rsa.VerifyPKCS1v15(pub, crypto.SHA512, sum[:], sig)
  if err != nil {
    t.Errorf("signature doesn't verify: %v", err)
  }

  var claims map[string]any
  raw, _ = base64.RawURLEncoding.DecodeString(parts[1])
  json.Unmarshal(raw, &claims)

  return claims
}

func TestKeyFlowSignsVerifiableJWTAndCachesToken(t *testing.T) {
  var exchanges int
  var key *rsa.PrivateKey
  server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    exchanges++
    raw, _ := io.ReadAll(r.Body)
    form, _ := url.ParseQuery(string(raw))
    if form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
      t.Errorf("grant_type = %q", form.Get("grant_type"))
    }

    claims := verifyJWT(t, form.Get("assertion"), &key.PublicKey)
    if claims["iss"] != "armin@sa.stackit.cloud" || claims["sub"] != "11111111-2222-3333-4444-555555555555" ||
      claims["jti"] == "" || claims["aud"] == "" {
      t.Errorf("claims = %v", claims)
    }

    exp, iat := claims["exp"].(float64), claims["iat"].(float64)
    if exp - iat != 600 {
      t.Errorf("exp - iat = %v, want 600", exp - iat)
    }

    io.WriteString(w, `{"access_token":"tok-1","expires_in":3600,"token_type":"Bearer"}`)
  }))
  defer server.Close()
  key = writeKeyFile(t, server.URL)

  ts, err := EnvTokenSource()
  if err != nil {
    t.Fatalf("EnvTokenSource: %v", err)
  }

  for i := 0; i < 3; i++ {
    tok, err := ts.Token(context.Background())
    if err != nil || tok != "tok-1" {
      t.Fatalf("Token = %q, %v", tok, err)
    }
  }

  if exchanges != 1 {
    t.Fatalf("exchanged %d times, want 1 (cached)", exchanges)
  }
}

func TestStaticTokenWins(t *testing.T) {
  t.Setenv("STACKIT_SERVICE_ACCOUNT_TOKEN", "static-tok")
  t.Setenv("STACKIT_SERVICE_ACCOUNT_KEY_PATH", "/nonexistent")
  ts, err := EnvTokenSource()
  if err != nil {
    t.Fatal(err)
  }

  tok, _ := ts.Token(context.Background())
  if tok != "static-tok" {
    t.Fatalf("Token = %q", tok)
  }
}

func TestNoCredentials(t *testing.T) {
  for _, k := range CredentialEnv {
    t.Setenv(k, "")
  }

  _, err := EnvTokenSource()
  if err != ErrNoCredentials {
    t.Fatalf("err = %v", err)
  }
}

// FakeIaaS is a one-server state machine speaking IaaS v2.
type FakeIaaS struct {
  mu      sync.Mutex
  Status  string
  Actions []string
}

func (f *FakeIaaS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
  f.mu.Lock()
  defer f.mu.Unlock()
  if r.Header.Get("Authorization") != "Bearer static-tok" {
    w.WriteHeader(401)
    io.WriteString(w, `{"code":401,"msg":"unauthorized"}`)

    return
  }

  base := "/v2/projects/p-1/regions/eu01/servers/s-1"
  switch {
  case r.Method == "GET" && r.URL.Path == base && r.URL.Query().Get("details") == "true":
    ip := ""
    if f.Status == "ACTIVE" {
      ip = `,"publicIp":"192.0.2.10"`
    }

    fmt.Fprintf(w, `{"id":"s-1","status":%q,"nics":[{"ipv4":"10.0.0.4"%s}]}`, f.Status, ip)
    if f.Status == "STARTING" {
      f.Status = "ACTIVE"
    }
  case r.Method == "POST" && r.URL.Path == base + "/start":
    f.Actions = append(f.Actions, "start")
    f.Status = "STARTING"
    w.WriteHeader(202)
  case r.Method == "POST" && r.URL.Path == base + "/deallocate":
    f.Actions = append(f.Actions, "deallocate")
    f.Status = "DEALLOCATED"
    w.WriteHeader(202)
  default:
    w.WriteHeader(404)
    fmt.Fprintf(w, `{"code":404,"msg":"no route %s %s"}`, r.Method, r.URL.Path)
  }
}

func TestWaitRunningStartsDeallocatedServer(t *testing.T) {
  StatePoll = 10 * time.Millisecond
  f := &FakeIaaS{ Status: "DEALLOCATED" }
  server := httptest.NewServer(f)
  defer server.Close()

  c := NewClient("p-1", "eu01", &TokenSource{ static: "static-tok" })
  c.Endpoint = server.URL

  ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
  defer cancel()
  srv, started, err := c.WaitRunning(ctx, "s-1", nil)
  if err != nil {
    t.Fatalf("WaitRunning: %v", err)
  }

  if !started || srv.Host() != "192.0.2.10" {
    t.Fatalf("started=%v host=%q", started, srv.Host())
  }

  err = c.DeallocateServer(ctx, "s-1")
  if err != nil {
    t.Fatal(err)
  }

  if strings.Join(f.Actions, ",") != "start,deallocate" {
    t.Fatalf("actions = %v", f.Actions)
  }
}

func TestAPIErrorIsReadable(t *testing.T) {
  server := httptest.NewServer(&FakeIaaS{ Status: "ACTIVE" })
  defer server.Close()
  c := NewClient("p-1", "eu01", &TokenSource{ static: "wrong" })
  c.Endpoint = server.URL
  _, err := c.GetServer(context.Background(), "s-1")
  if err == nil || !strings.Contains(err.Error(), "HTTP 401: unauthorized") {
    t.Fatalf("err = %v", err)
  }
}
