// Package searxng manages a local SearXNG instance for the
// web_search tool: a Docker container started lazily on first
// search and stopped when armin exits — but only if this process is
// the one that started it, so it never kills a container the user
// is already running for something else.
package searxng

import (
  "bytes"
  "context"
  "crypto/rand"
  "encoding/hex"
  "encoding/json"
  "fmt"
  "net/http"
  "net/url"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "sync"
  "time"

  "armin/internal/config"
)

const (
  containerName = "armin-searxng"
  image         = "docker.io/searxng/searxng:latest"
  port          = 18080
  baseURL       = "http://localhost:18080"

  startTimeout  = 90 * time.Second
  stopTimeout   = 15 * time.Second
  inspectTimeout = 10 * time.Second
  queryTimeout  = 15 * time.Second
  readyPoll     = 500 * time.Millisecond

  // maxSnippetChars bounds each result's snippet so one search can't
  // eat a meaningful slice of a small local model's context budget.
  maxSnippetChars = 200
)

// Result is one search hit, already trimmed down to what's worth
// spending context on.
type Result struct {
  Title   string
  URL     string
  Snippet string
}

// Manager owns the container's lifecycle. Zero value is ready to
// use; construct with New.
type Manager struct {
  mu          sync.Mutex
  ready       bool
  startedByUs bool
}

// New returns a Manager that hasn't touched Docker yet — nothing
// starts until the first Search call.
func New() *Manager {
  return &Manager{}
}

// Search ensures the container is running, then queries it and
// returns a compact result set: title, URL, and a short snippet per
// hit, never full page bodies.
func (m *Manager) Search(
  ctx context.Context, query string, count int,
) ([]Result, error) {
  if err := m.ensureRunning(ctx); err != nil {
    return nil, fmt.Errorf("\n\t... in Search:\n\t%w", err)
  }

  q := url.Values{}
  q.Set("q", query)
  q.Set("format", "json")

  req, err := http.NewRequestWithContext(
    ctx, http.MethodGet, baseURL+"/search?"+q.Encode(), nil,
  )
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Search:\n\t%w", err)
  }

  client := &http.Client{ Timeout: queryTimeout }
  resp, err := client.Do(req)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t querying searxng: %w", err,
    )
  }
  defer resp.Body.Close()

  if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t searxng returned %s", resp.Status,
    )
  }

  var parsed searxResponse
  if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t decoding searxng response: %w", err,
    )
  }

  return toResults(parsed, count), nil
}

// Shutdown stops the container, but only if this Manager is the one
// that started it — a container the user already had running (or
// left over from a previous armin run) is never touched.
func (m *Manager) Shutdown(ctx context.Context) error {
  m.mu.Lock()
  defer m.mu.Unlock()

  if !m.startedByUs {
    return nil
  }

  err := runDocker(ctx, stopTimeout, "stop", containerName)
  m.ready = false
  m.startedByUs = false
  return err
}

func (m *Manager) ensureRunning(ctx context.Context) error {
  m.mu.Lock()
  defer m.mu.Unlock()

  if m.ready {
    return nil
  }

  exists, running, err := containerState(ctx)
  if err != nil {
    return err
  }

  switch {
  case running:
    // Already up — not something we started, so Shutdown must
    // leave it alone.
  case exists:
    if err := runDocker(ctx, startTimeout, "start", containerName); err != nil {
      return err
    }
    m.startedByUs = true
  default:
    settingsPath, err := ensureSettings()
    if err != nil {
      return fmt.Errorf("\n\t... in ensureRunning:\n\t%w", err)
    }
    err = runDocker(ctx, startTimeout, "run", "-d",
      "--name", containerName,
      "-p", fmt.Sprintf("%d:8080", port),
      "-v", settingsPath+":/etc/searxng/settings.yml:ro",
      image,
    )
    if err != nil {
      return err
    }
    m.startedByUs = true
  }

  if err := waitReady(ctx); err != nil {
    return err
  }
  m.ready = true
  return nil
}

func waitReady(ctx context.Context) error {
  ctx, cancel := context.WithTimeout(ctx, startTimeout)
  defer cancel()

  client := &http.Client{ Timeout: 3 * time.Second }

  for {
    req, err := http.NewRequestWithContext(
      ctx, http.MethodGet, baseURL+"/", nil,
    )
    if err == nil {
      if resp, err := client.Do(req); err == nil {
        resp.Body.Close()
        if resp.StatusCode < 500 {
          return nil
        }
      }
    }

    select {
    case <-ctx.Done():
      return fmt.Errorf(
        "\n\t... in waitReady:\n\t timed out waiting for searxng to come up",
      )
    case <-time.After(readyPoll):
    }
  }
}

// containerState reports whether containerName exists at all, and
// if so whether it's currently running.
func containerState(ctx context.Context) (exists, running bool, err error) {
  ctx, cancel := context.WithTimeout(ctx, inspectTimeout)
  defer cancel()

  cmd := exec.CommandContext(
    ctx, "docker", "inspect", "-f", "{{.State.Running}}", containerName,
  )
  var stdout, stderr bytes.Buffer
  cmd.Stdout = &stdout
  cmd.Stderr = &stderr

  if err := cmd.Run(); err != nil {
    if strings.Contains(stderr.String(), "No such") {
      return false, false, nil
    }
    return false, false, fmt.Errorf(
      "\n\t... in containerState:\n\t docker inspect: %s",
      strings.TrimSpace(stderr.String()),
    )
  }

  return true, strings.TrimSpace(stdout.String()) == "true", nil
}

func runDocker(ctx context.Context, timeout time.Duration, args ...string) error {
  ctx, cancel := context.WithTimeout(ctx, timeout)
  defer cancel()

  cmd := exec.CommandContext(ctx, "docker", args...)
  var stderr bytes.Buffer
  cmd.Stderr = &stderr

  if err := cmd.Run(); err != nil {
    if ctx.Err() == context.DeadlineExceeded {
      return fmt.Errorf(
        "\n\t... in runDocker:\n\t docker %s timed out after %s",
        args[0], timeout,
      )
    }
    return fmt.Errorf(
      "\n\t... in runDocker:\n\t docker %s: %s",
      strings.Join(args, " "), strings.TrimSpace(stderr.String()),
    )
  }
  return nil
}

// searxResponse is the slice of SearXNG's ?format=json body we
// actually use.
type searxResponse struct {
  Results []struct {
    URL     string `json:"url"`
    Title   string `json:"title"`
    Content string `json:"content"`
  } `json:"results"`
}

func toResults(parsed searxResponse, count int) []Result {
  if count <= 0 || count > len(parsed.Results) {
    count = len(parsed.Results)
  }

  results := make([]Result, count)
  for i := 0; i < count; i++ {
    r := parsed.Results[i]
    results[i] = Result{
      Title:   r.Title,
      URL:     r.URL,
      Snippet: truncateSnippet(r.Content),
    }
  }
  return results
}

func truncateSnippet(s string) string {
  if len(s) <= maxSnippetChars {
    return s
  }
  return s[:maxSnippetChars] + "…"
}

// settingsTemplate enables SearXNG's JSON output format, which is
// off by default (the public-facing anti-scraping default) — armin
// only ever talks to its own private, localhost-only instance, so
// there's no abuse surface to guard against here. secret_key is
// required by SearXNG for CSRF tokens; %s is a random 32-byte hex
// string generated once and reused across restarts.
const settingsTemplate = `use_default_settings: true

server:
  secret_key: "%s"

search:
  formats:
    - html
    - json
`

// ensureSettings writes a settings.yml enabling JSON output the
// first time it's needed, and returns its path. Later calls reuse
// the same file (and the same secret_key) rather than regenerating
// it on every container start.
func ensureSettings() (string, error) {
  dir, err := config.Dir()
  if err != nil {
    return "", fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  searxDir := filepath.Join(dir, "searxng")
  if err := os.MkdirAll(searxDir, 0o755); err != nil {
    return "", fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  path := filepath.Join(searxDir, "settings.yml")
  if _, err := os.Stat(path); err == nil {
    return path, nil
  } else if !os.IsNotExist(err) {
    return "", fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  key := make([]byte, 32)
  if _, err := rand.Read(key); err != nil {
    return "", fmt.Errorf(
      "\n\t... in ensureSettings:\n\t generating secret_key: %w", err,
    )
  }

  content := fmt.Sprintf(settingsTemplate, hex.EncodeToString(key))
  if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
    return "", fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  return path, nil
}
