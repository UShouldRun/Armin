// What a model spec means: its window (contextOf, withContext), its
// tiers (inTier), whether armin runs it (managed), its URL and label,
// and the llama-server arguments it starts with (serverArgs).

package llamacpp

import (
  "fmt"
  "path/filepath"
  "strconv"
  "strings"
  "time"
)

// contextFlags are llama-server's names for the window size.
var contextFlags = map[string]bool{ "-c": true, "--ctx-size": true }

// contextOf reads -c / --ctx-size out of args; 0 if absent.
func contextOf(args []string) int {
  for i, a := range args {
    name, val, hasVal := strings.Cut(a, "=")
    if !contextFlags[name] {
      continue
    }

    if !hasVal && i + 1 < len(args) {
      val = args[i + 1]
    }

    n, _ := strconv.Atoi(val)

    return n
  }

  return 0
}

// withContext returns args with the window set to n.
func withContext(args []string, n int) []string {
  out := make([]string, 0, len(args) + 2)
  for i := 0; i < len(args); i++ {
    name, _, hasVal := strings.Cut(args[i], "=")
    if contextFlags[name] {
      if !hasVal {
        i++ // skip the value too
      }

      continue
    }

    out = append(out, args[i])
  }

  return append(out, "-c", strconv.Itoa(n))
}

// inTier is the spec as it runs in t: the stream tier swaps in the
// fork's binary, its window and its arena.
func (s ModelSpec) inTier(t Tier) ModelSpec {
  if t != TierStream || s.Stream == nil {
    return s
  }

  st := s.Stream
  if st.Binary != "" {
    s.Binary = st.Binary
  }

  args := withContext(s.Args, st.Context)
  if st.ArenaMiB > 0 {
    args = append(args, "--kv-stream-arena-mib", strconv.Itoa(st.ArenaMiB))
  }

  s.Args = append(args, st.Args...)

  return s
}

func (s ModelSpec) managed() bool { return s.URL == "" }

func (s ModelSpec) idleTimeout() time.Duration {
  if s.IdleTimeout != 0 {
    return s.IdleTimeout
  }

  return DefaultIdleTimeout
}

func (s ModelSpec) url() string {
  if !s.managed() {
    return s.URL
  }

  return fmt.Sprintf("http://127.0.0.1:%d", s.Port)
}

// label names what serves the spec: the model file (without its
// directory and .gguf), plus the machine for a cloud one; the URL for
// an external endpoint.
func (s ModelSpec) label() string {
  if !s.managed() && s.ID != "" {
    return s.ID
  }

  if !s.managed() {
    return s.URL
  }

  file := strings.TrimSuffix(filepath.Base(s.ModelPath), ".gguf")
  if s.Remote != nil {
    return file + " on " + s.Remote.Provider.String() + " " + s.Remote.MachineID
  }

  return file
}

// serverArgs is spec.Args plus every lean default the args don't
// already decide (the projector one is skipped with SpecVision).
func serverArgs(spec ModelSpec) []string {
  args := append([]string{}, spec.Args...)
  if spec.Flags & SpecNoLean != 0 {
    return args
  }

  set := make(map[string]bool, len(args))
  for _, a := range args {
    name, _, _ := strings.Cut(a, "=")
    set[name] = true
  }

  for i, d := range leanDefaults {
    if i == 0 && spec.Flags & SpecVision != 0 {
      continue
    }

    decided := false
    for _, f := range d.flags {
      if set[f] {
        decided = true
        break
      }
    }

    if !decided {
      args = append(args, d.args...)
    }
  }

  return args
}
