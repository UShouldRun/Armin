// Package metrics measures armin's deterministic systems (the tree's
// search and recall, staleness, dedup, delegation checks, the tools
// themselves) so it can be seen how much each helps the models.
//
//   Add       count something (a recall attached, a check passed)
//   Observe   record a measured value (milliseconds, bytes, nodes)
//   Event     a detailed record; only in debug builds (-tags debug),
//             compiled to nothing otherwise
//   Flush     merge this process's counts into the cumulative file
//   Report    print the cumulative file with the derived rates
//
// Release builds keep counts in memory and merge them into
// <data>/armin/metrics/metrics.tsv at shutdown: one line per metric,
// name, count, sum, min, max, tab-separated. Debug builds also append
// every event to events-<date>.tsv as it happens. Nothing leaves the
// machine.
package metrics

import (
  "bufio"
  "fmt"
  "math"
  "os"
  "path/filepath"
  "sort"
  "strconv"
  "strings"
  "time"
)

var reg = registry{ stats: map[string]*stat{} }

// dir is where Flush and Event write; set by SetDir at startup, empty
// (nothing written) in tests and tools that never set it.
var dir string

// SetDir sets the directory metrics are kept in.
func SetDir(d string) {
  reg.mu.Lock()
  dir = d
  reg.mu.Unlock()
}

func (r *registry) get(name string) *stat {
  s := r.stats[name]
  if s == nil {
    s = &stat{ Min: math.Inf(1), Max: math.Inf(-1) }
    r.stats[name] = s
  }

  return s
}

// Add counts n occurrences of name.
func Add(name string, n int64) {
  reg.mu.Lock()
  reg.get(name).Count += n
  reg.mu.Unlock()
}

// Observe records one measured value of name.
func Observe(name string, v float64) {
  reg.mu.Lock()
  s := reg.get(name)
  s.Count++
  s.Sum += v
  s.Min = math.Min(s.Min, v)
  s.Max = math.Max(s.Max, v)
  reg.mu.Unlock()
}

// Since observes the milliseconds since start under name.
func Since(name string, start time.Time) {
  Observe(name, float64(time.Since(start).Microseconds()) / 1000)
}

// Event records a detailed event in debug builds: name and key=value
// pairs, appended to the day's events file. A no-op in release builds.
func Event(name string, kv ...any) {
  if !Debug {
    return
  }

  reg.mu.Lock()
  d := dir
  reg.mu.Unlock()
  if d == "" {
    return
  }

  var b strings.Builder
  fmt.Fprintf(&b, "%d\t%s", time.Now().UnixMilli(), name)
  for i := 0; i + 1 < len(kv); i += 2 {
    fmt.Fprintf(&b, "\t%v=%v", kv[i], kv[i + 1])
  }

  b.WriteByte('\n')
  path := filepath.Join(d, "events-" + time.Now().Format("2006-01-02") + ".tsv")
  f, err := os.OpenFile(path, os.O_WRONLY | os.O_CREATE | os.O_APPEND, 0o600)
  if err != nil {
    return
  }

  f.WriteString(b.String())
  f.Close()
}

// Snapshot returns a copy of this process's metrics, for tests and
// the benchmark.
func Snapshot() map[string]stat {
  reg.mu.Lock()
  defer reg.mu.Unlock()

  out := make(map[string]stat, len(reg.stats))
  for k, v := range reg.stats {
    out[k] = *v
  }

  return out
}

// Reset clears this process's metrics.
func Reset() {
  reg.mu.Lock()
  reg.stats = map[string]*stat{}
  reg.mu.Unlock()
}

// read loads a cumulative file; a missing or damaged one is empty.
func read(path string) map[string]*stat {
  out := map[string]*stat{}
  f, err := os.Open(path)
  if err != nil {
    return out
  }

  defer f.Close()

  sc := bufio.NewScanner(f)
  for sc.Scan() {
    p := strings.Split(sc.Text(), "\t")
    if len(p) != 5 {
      continue
    }

    s := &stat{}
    s.Count, _ = strconv.ParseInt(p[1], 10, 64)
    s.Sum, _ = strconv.ParseFloat(p[2], 64)
    s.Min, _ = strconv.ParseFloat(p[3], 64)
    s.Max, _ = strconv.ParseFloat(p[4], 64)
    out[p[0]] = s
  }

  return out
}

// Flush merges this process's metrics into metrics.tsv, through a
// temporary file and a rename, and clears them.
func Flush() error {
  reg.mu.Lock()
  defer reg.mu.Unlock()

  if dir == "" || len(reg.stats) == 0 {
    return nil
  }

  err := os.MkdirAll(dir, 0o700)
  if err != nil {
    return fmt.Errorf("\n\t... in Flush:\n\t%w", err)
  }

  path := filepath.Join(dir, "metrics.tsv")
  all := read(path)
  for name, s := range reg.stats {
    t := all[name]
    if t == nil {
      all[name] = &stat{ Count: s.Count, Sum: s.Sum, Min: s.Min, Max: s.Max }
      continue
    }

    t.Count += s.Count
    t.Sum += s.Sum
    t.Min = math.Min(t.Min, s.Min)
    t.Max = math.Max(t.Max, s.Max)
  }

  var b strings.Builder
  for _, name := range sorted(all) {
    s := all[name]
    fmt.Fprintf(&b, "%s\t%d\t%g\t%g\t%g\n", name, s.Count, s.Sum, s.Min, s.Max)
  }

  tmp := path + ".tmp"
  err = os.WriteFile(tmp, []byte(b.String()), 0o600)
  if err == nil {
    err = os.Rename(tmp, path)
  }

  if err != nil {
    return fmt.Errorf("\n\t... in Flush:\n\t%w", err)
  }

  reg.stats = map[string]*stat{}

  return nil
}

func sorted(m map[string]*stat) []string {
  names := make([]string, 0, len(m))
  for k := range m {
    names = append(names, k)
  }

  sort.Strings(names)

  return names
}
