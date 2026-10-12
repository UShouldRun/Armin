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

// New returns a Manager that hasn't touched Docker yet — nothing
// starts until the first Search call.
func New() *Manager {
  return &Manager{}
}

// Search ensures the container is running, then queries it and
// returns a compact result set: title, URL, and a short snippet per
// hit, never full page bodies.
func (m *Manager) Search(
  ctx context.Context, query string, count int, topic string,
) ([]Result, error) {
  err := m.ensureRunning(ctx)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Search:\n\t%w", err)
  }

  q := url.Values{}
  q.Set("q", query)
  q.Set("format", "json")
  q.Set("categories", Category(topic))

  req, err := http.NewRequestWithContext(
    ctx, http.MethodGet, baseURL + "/search?" + q.Encode(), nil,
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
  err = json.NewDecoder(resp.Body).Decode(&parsed)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t decoding searxng response: %w", err,
    )
  }

  results := toResults(parsed, query, count)

  // No results while engines fail is a degraded search, not an
  // answer: say which engines failed, so the model reports it rather
  // than concluding nothing exists.
  if len(results) == 0 && len(parsed.Unresponsive) > 0 {
    var failed []string
    for _, u := range parsed.Unresponsive {
      if len(u) == 2 {
        failed = append(failed, u[0] + ": " + u[1])
      }
    }

    return nil, fmt.Errorf(
      "\n\t... in Search:\n\t no results, and these search engines "+
        "failed: %s. Web search is degraded right now: try topic "+
        "general or a simpler query, and tell the user if it keeps "+
        "failing rather than answering as if you had searched",
      strings.Join(failed, ", "),
    )
  }

  return results, nil
}

// Category maps a web_search topic to SearXNG categories: general web
// pages, programming Q&A and code, or papers.
//
// code and science add the general engines too: documentation sites
// are general results, and a rate-limited GitHub mustn't leave a
// code search with nothing.
func Category(topic string) string {
  switch topic {
  case "code":
    return "general,it"
  case "science":
    return "science,general"
  }

  return "general"
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

  // Settings first: a rewrite (new engine list) must reach a
  // container that's already up, too.
  settingsPath, changed, err := ensureSettings()
  if err != nil {
    return fmt.Errorf("\n\t... in ensureRunning:\n\t%w", err)
  }

  exists, running, err := containerState(ctx)
  if err != nil {
    return err
  }

  switch {
  case running && changed:
    // The settings file is bind-mounted; a restart rereads it.
    err := runDocker(ctx, startTimeout, "restart", containerName)
    if err != nil {
      return err
    }
  case running:
    // Already up — not something we started, so Shutdown must
    // leave it alone.
  case exists:
    err := runDocker(ctx, startTimeout, "start", containerName)
    if err != nil {
      return err
    }

    m.startedByUs = true
  default:
    err = runDocker(ctx, startTimeout, "run", "-d",
      "--name", containerName,
      "-p", fmt.Sprintf("%d:8080", port),
      "-v", settingsPath + ":/etc/searxng/settings.yml:ro",
      image,
    )
    if err != nil {
      return err
    }

    m.startedByUs = true
  }

  err = waitReady(ctx)
  if err != nil {
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
      ctx, http.MethodGet, baseURL + "/", nil,
    )
    if err == nil {
      resp, err := client.Do(req)
      if err == nil {
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

  err = cmd.Run()
  if err != nil {
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

  err := cmd.Run()
  if err != nil {
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

// toResults keeps up to count relevant results: an engine that
// ignores the query (a shop's homepage for an MPI search) shares no
// meaningful word with it, and is dropped.
func toResults(parsed searxResponse, query string, count int) []Result {
  terms := queryTerms(query)
  if count <= 0 {
    count = len(parsed.Results)
  }

  results := make([]Result, 0, min(count, len(parsed.Results)))
  for _, r := range parsed.Results {
    if len(results) == count {
      break
    }

    if !relevant(terms, r.Title + " " + r.Content + " " + r.URL) {
      continue
    }

    results = append(results, Result{
      Title:   r.Title,
      URL:     r.URL,
      Snippet: truncateSnippet(r.Content),
    })
  }

  return results
}

// queryTerms are the words of a query worth matching on: 3+
// characters, lowercased, without quotes or operators.
func queryTerms(query string) []string {
  split := func(r rune) bool {
    return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r > 127)
  }

  var out []string
  for _, w := range strings.FieldsFunc(strings.ToLower(query), split) {
    if len(w) >= 3 && !stopWords[w] {
      out = append(out, w)
    }
  }

  return out
}

var stopWords = map[string]bool{
  "the": true, "and": true, "for": true, "with": true, "how": true,
  "what": true, "from": true, "into": true, "using": true, "use": true,
}

// relevant: a query without usable terms keeps everything.
func relevant(terms []string, text string) bool {
  if len(terms) == 0 {
    return true
  }

  text = strings.ToLower(text)
  for _, t := range terms {
    if strings.Contains(text, t) {
      return true
    }
  }

  return false
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
//
// The engines are chosen by testing, not taken from SearXNG's
// defaults: from a home connection DuckDuckGo, Startpage, Brave,
// Qwant and Karmasearch answer with CAPTCHAs or bans, and an empty
// result set looks to the model like "nothing exists". keep_only
// drops the rest. Bump settingsVersion when this changes: an older
// file is rewritten (keeping its secret_key).
const settingsTemplate = `# armin-searxng v%d — generated by armin; edits are overwritten when
# armin updates this file (delete the version line above to keep yours).
use_default_settings:
  engines:
    keep_only:
      - yandex
      - bing
      - wikipedia
      - stackoverflow
      - github
      - arxiv
      - semantic scholar
      - openalex

server:
  secret_key: "%s"
  limiter: false

search:
  # English results: with "auto", Bing answers queries it doesn't
  # understand with popular local pages (a Portuguese store's site for
  # an MPI search).
  default_lang: "en"
  formats:
    - html
    - json

engines:
  - name: yandex
    disabled: false
  - name: bing
    disabled: false
  - name: wikipedia
    disabled: false
  - name: github
    disabled: false
  - name: semantic scholar
    disabled: false
  - name: openalex
    disabled: false
`

// settingsVersion is the generation of settingsTemplate.
const settingsVersion = 3

// settingsHeader is how a generated file starts.
const settingsHeader = "# armin-searxng v"

// ensureSettings writes settings.yml the first time it's needed, or
// rewrites an outdated generated one (keeping its secret_key), and
// returns its path and whether it changed.
func ensureSettings() (string, bool, error) {
  dir, err := config.Dir()
  if err != nil {
    return "", false, fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  searxDir := filepath.Join(dir, "searxng")
  err = os.MkdirAll(searxDir, 0o755)
  if err != nil {
    return "", false, fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  path := filepath.Join(searxDir, "settings.yml")
  old, err := os.ReadFile(path)
  if err != nil && !os.IsNotExist(err) {
    return "", false, fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  current := fmt.Sprintf("%s%d ", settingsHeader, settingsVersion)
  if err == nil && !outdated(string(old), current) {
    return path, false, nil
  }

  secret := secretOf(string(old))
  if secret == "" {
    key := make([]byte, 32)
    _, err := rand.Read(key)
    if err != nil {
      return "", false, fmt.Errorf(
        "\n\t... in ensureSettings:\n\t generating secret_key: %w", err,
      )
    }

    secret = hex.EncodeToString(key)
  }

  content := fmt.Sprintf(settingsTemplate, settingsVersion, secret)
  err = os.WriteFile(path, []byte(content), 0o644)
  if err != nil {
    return "", false, fmt.Errorf("\n\t... in ensureSettings:\n\t%w", err)
  }

  return path, true, nil
}

// outdated: an armin-generated file from an older version, or the
// original unversioned template. A file the user wrote themselves
// (no header, not the old template) is left alone.
func outdated(content, current string) bool {
  if strings.HasPrefix(content, current) {
    return false
  }

  if strings.HasPrefix(content, settingsHeader) {
    return true
  }

  return strings.HasPrefix(content, "use_default_settings: true\n\nserver:\n  secret_key:") &&
    strings.Count(content, "\n") <= 9
}

// secretOf pulls secret_key out of an existing file, so rewriting it
// doesn't invalidate SearXNG's tokens.
func secretOf(content string) string {
  for _, line := range strings.Split(content, "\n") {
    v, ok := strings.CutPrefix(strings.TrimSpace(line), "secret_key:")
    if ok {
      return strings.Trim(strings.TrimSpace(v), `"`)
    }
  }

  return ""
}
