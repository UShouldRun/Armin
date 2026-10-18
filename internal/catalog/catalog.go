// Package catalog suggests hosted models to plan and research with:
// OpenRouter's public model list, narrowed to what armin can drive
// (tool calling), priced by what a deep session costs. Fetched in
// the background and cached a day on disk, so no UI ever waits on
// it.
//
//   catalog.go  Warm (fetch in the background), Suggestions and Price
//               for the pickers, Credits for the balance line, Pick
//               (which models to suggest)
//   region.go   Regional and Global: OpenRouter's in-region host for
//               the user's time zone, and the global one to fall
//               back to
//
// Read by repl's /model and display's picker; llamacpp uses region.go
// for a hosted model's URL.
package catalog

import (
  "context"
  "encoding/json"
  "fmt"
  "io"
  "net/http"
  "os"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "sync"
  "time"

  "armin/internal/session"
)

const (
  listURL  = "https://openrouter.ai/api/v1/models"
  maxAge   = 24 * time.Hour
  fetchMax = 10 * time.Second

  // A deep planning session, as armin runs one: ~30 tool rounds
  // resending the conversation (input), a few long answers (output).
  SessionIn  = 1_200_000
  SessionOut = 30_000

  maxPlanners   = 16
  budgetSession = 0.30
  maxFree     = 3

  // recentSecs is how far behind a vendor's newest model a cheaper
  // one may be and still count as current (about four months).
  recentSecs = 120 * 24 * 3600
)

// vendors whose newest strong model is suggested as a planner.
var vendors = []string{
  "qwen", "deepseek", "z-ai", "moonshotai", "minimax", "anthropic",
  "openai", "google", "x-ai", "mistralai", "nvidia", "xiaomi",
  "stepfun",
}

// light marks a vendor's small, fast tier: fine for quick questions,
// not what you'd plan with.
var light = []string{
  "flash", "lite", "mini", "nano", "omni", "haiku", "luna", "small",
  "vision", "image", "banana", "lightning", "audio", "tts", "embed",
}

var (
  mu      sync.Mutex
  current []Suggestion
  prices  map[string]Suggestion // every usable model, by id
  warming bool

  balanceMu  sync.Mutex
  balance    *Balance
  balanceAt  time.Time
  refreshing bool
)

// balanceAge: a balance this fresh isn't asked for again.
const balanceAge = time.Minute

// Warm loads the cache from disk and, when it's older than a day,
// refreshes it from OpenRouter in the background.
func Warm() {
  mu.Lock()
  if warming {
    mu.Unlock()
    return
  }

  warming = true
  mu.Unlock()

  go func() {
    path := cachePath()
    data, err := os.ReadFile(path)
    info, statErr := os.Stat(path)
    if err == nil {
      set(data)
    }

    fresh := statErr == nil && time.Since(info.ModTime()) < maxAge
    if fresh {
      return
    }

    data, err = fetch()
    if err != nil {
      return
    }

    set(data)
    os.MkdirAll(filepath.Dir(path), 0o700)
    os.WriteFile(path, data, 0o600)
  }()
}

// Load reads the day's cache from disk now, without the network:
// for a question asked before anything connects.
func Load() {
  data, err := os.ReadFile(cachePath())
  if err == nil {
    set(data)
  }
}

// Suggestions is what's cached now, planners by session cost then
// free ones; empty until Warm has something.
func Suggestions() []Suggestion {
  mu.Lock()
  defer mu.Unlock()

  return current
}

func cachePath() string {
  dir, err := session.DataDir()
  if err != nil {
    dir = os.TempDir()
  }

  return filepath.Join(dir, "catalog", "openrouter.json")
}

func fetch() ([]byte, error) {
  ctx, cancel := context.WithTimeout(context.Background(), fetchMax)
  defer cancel()

  req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in fetch:\n\t%w", err)
  }

  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in fetch:\n\t%w", err)
  }

  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    return nil, fmt.Errorf(
      "\n\t... in fetch:\n\t openrouter returned %d", resp.StatusCode,
    )
  }

  return io.ReadAll(io.LimitReader(resp.Body, 32 << 20))
}

func set(data []byte) {
  list, err := Pick(data)
  if err != nil || len(list) == 0 {
    return
  }

  all := map[string]Suggestion{}
  var body struct {
    Data []orModel `json:"data"`
  }

  json.Unmarshal(data, &body)
  for _, m := range body.Data {
    all[m.ID] = suggestion(m)
  }

  mu.Lock()
  current, prices = list, all
  mu.Unlock()
}

// Price is what model id costs, once the list is loaded.
func Price(id string) (Suggestion, bool) {
  mu.Lock()
  defer mu.Unlock()
  s, ok := prices[id]

  return s, ok
}

// CachedCredits is the last balance asked, without waiting: when it's
// older than a minute (or there's none yet) it asks again in the
// background, for the next caller. Nil until the first answer.
func CachedCredits(base, key string) *Balance {
  balanceMu.Lock()
  b, stale := balance, balance == nil || time.Since(balanceAt) >= balanceAge
  asking := refreshing
  if stale && !asking {
    refreshing = true
  }

  balanceMu.Unlock()

  if stale && !asking {
    go func() {
      ctx, cancel := context.WithTimeout(context.Background(), fetchMax)
      defer cancel()
      Credits(ctx, base, key)

      balanceMu.Lock()
      refreshing = false
      balanceMu.Unlock()
    }()
  }

  return b
}

// Credits is the account's balance through key: what's left of the
// credits bought, today's spend, and the key's own cap. Asked at
// most once a minute; base is the configured OpenRouter URL.
func Credits(ctx context.Context, base, key string) (*Balance, error) {
  balanceMu.Lock()
  defer balanceMu.Unlock()

  if balance != nil && time.Since(balanceAt) < balanceAge {
    return balance, nil
  }

  var credits struct {
    Data struct {
      Total float64 `json:"total_credits"`
      Usage float64 `json:"total_usage"`
    } `json:"data"`
  }

  err := getJSON(ctx, apiURL(base, "/credits"), key, &credits)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Credits:\n\t%w", err)
  }

  var info struct {
    Data struct {
      Limit     *float64 `json:"limit"`
      Remaining *float64 `json:"limit_remaining"`
      Daily     float64  `json:"usage_daily"`
    } `json:"data"`
  }

  b := &Balance{ Credits: credits.Data.Total, Used: credits.Data.Usage }
  err = getJSON(ctx, apiURL(base, "/key"), key, &info)
  if err == nil {
    b.Today = info.Data.Daily
    if info.Data.Limit != nil && info.Data.Remaining != nil {
      b.KeyLimit, b.KeyLeft = *info.Data.Limit, *info.Data.Remaining
    }
  }

  balance, balanceAt = b, time.Now()

  return b, nil
}

// apiURL joins path onto an OpenRouter base with or without /v1.
func apiURL(base, path string) string {
  base = strings.TrimSuffix(base, "/")
  if !strings.HasSuffix(base, "/v1") {
    base += "/v1"
  }

  return base + path
}

func getJSON(ctx context.Context, url, key string, out any) error {
  req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
  if err != nil {
    return fmt.Errorf("\n\t... in getJSON:\n\t%w", err)
  }

  req.Header.Set("Authorization", "Bearer " + key)
  resp, err := http.DefaultClient.Do(req)
  if err != nil {
    return fmt.Errorf("\n\t... in getJSON:\n\t%w", err)
  }

  defer resp.Body.Close()
  if resp.StatusCode != http.StatusOK {
    return fmt.Errorf(
      "\n\t... in getJSON:\n\t openrouter returned %d", resp.StatusCode,
    )
  }

  err = json.NewDecoder(io.LimitReader(resp.Body, 1 << 20)).Decode(out)
  if err != nil {
    return fmt.Errorf("\n\t... in getJSON:\n\t%w", err)
  }

  return nil
}

// Pick chooses the suggestions out of OpenRouter's /models answer.
// Per vendor, among its tool-capable models that aren't a light
// tier: the newest (its strongest, usually), and also its cheapest
// recent one when a session on it costs under budgetSession. By
// session cost, then the newest free models of the same vendors.
func Pick(data []byte) ([]Suggestion, error) {
  var body struct {
    Data []orModel `json:"data"`
  }

  err := json.Unmarshal(data, &body)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Pick:\n\t%w", err)
  }

  byVendor := map[string][]orModel{}
  var free []orModel
  for _, m := range body.Data {
    vendor, _, _ := strings.Cut(m.ID, "/")
    if !usable(m) || !contains(vendors, vendor) {
      continue
    }

    if strings.HasSuffix(m.ID, ":free") {
      free = append(free, m)
      continue
    }

    if !isLight(m.ID) {
      byVendor[vendor] = append(byVendor[vendor], m)
    }
  }

  var out []Suggestion
  for _, list := range byVendor {
    newest := list[0]
    for _, m := range list {
      if m.Created > newest.Created {
        newest = m
      }
    }

    cheapest := newest
    for _, m := range list {
      recent := m.Created >= newest.Created - recentSecs
      if recent && sessionCost(m) < sessionCost(cheapest) {
        cheapest = m
      }
    }

    out = append(out, planner(newest))
    if cheapest.ID != newest.ID && sessionCost(cheapest) < budgetSession {
      out = append(out, planner(cheapest))
    }
  }

  sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
  if len(out) > maxPlanners {
    out = out[:maxPlanners]
  }

  sort.Slice(free, func(i, j int) bool { return free[i].Created > free[j].Created })
  for i := 0; i < len(free) && i < maxFree; i++ {
    s := suggestion(free[i])
    s.Free = true
    out = append(out, s)
  }

  return out, nil
}

func planner(m orModel) Suggestion {
  s := suggestion(m)
  s.Planner = true

  return s
}

func sessionCost(m orModel) float64 { return suggestion(m).Session }

// usable: armin needs tool calling, and an answer now (no :batch).
func usable(m orModel) bool {
  return contains(m.Params, "tools") && !strings.HasSuffix(m.ID, ":batch")
}

func isLight(id string) bool {
  lower := strings.ToLower(id)
  for _, l := range light {
    if strings.Contains(lower, l) {
      return true
    }
  }

  return false
}

func contains(list []string, s string) bool {
  for _, v := range list {
    if v == s {
      return true
    }
  }

  return false
}

func suggestion(m orModel) Suggestion {
  in, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
  out, _ := strconv.ParseFloat(m.Pricing.Completion, 64)

  return Suggestion{
    ID:      m.ID,
    In:      in * 1e6,
    Out:     out * 1e6,
    Context: m.Context,
    Session: in * SessionIn + out * SessionOut,
  }
}
