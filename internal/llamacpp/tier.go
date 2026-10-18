// The two context tiers of a model with a streaming build: resident
// (the whole KV cache in VRAM, fastest) and stream (KV cache streamed
// from RAM, largest). SetTier restarts the server in the other one;
// Window is the agent loop's handle on it (see agent/context.go).

package llamacpp

import (
  "context"
  "fmt"
)

const (
  TierResident Tier = 1 << iota // whole KV cache in VRAM: fastest
  TierStream                    // KV cache streamed from RAM: largest
  TierAll      = TierResident | TierStream
)

// SetTier restarts name's process in tier t, unless it's already
// there. The URL doesn't change, so every Client keeps working.
func (m *Manager) SetTier(ctx context.Context, name string, t Tier) error {
  m.mu.Lock()
  spec, ok := m.specs[name]
  if !ok || spec.Stream == nil {
    m.mu.Unlock()
    return fmt.Errorf("\n\t... in SetTier:\n\t model %q has no stream tier", name)
  }

  cur := m.tiers[name]
  if cur == 0 {
    cur = TierResident
  }

  if cur == t {
    m.mu.Unlock()
    return nil
  }

  proc := m.procs[name]
  delete(m.procs, name)
  m.tiers[name] = t
  m.mu.Unlock()

  if proc != nil {
    // Keep a remote machine: only the server on it restarts.
    remote := proc.remote
    proc.remote = nil
    proc.stop(ctx)
    if remote != nil && remote.started {
      m.mu.Lock()
      m.owned[remote.machine.ID()] = remote
      m.mu.Unlock()
    }
  }

  _, err := m.ensure(ctx, name)
  if err != nil {
    return fmt.Errorf("\n\t... in SetTier:\n\t%w", err)
  }

  return nil
}

// Window returns name's tier control, or nil when the model has no
// stream tier or no explicit -c to size the resident one.
func (m *Manager) Window(name string) *Window {
  m.mu.Lock()
  defer m.mu.Unlock()
  spec, ok := m.specs[name]
  if !ok || spec.Stream == nil {
    return nil
  }

  resident := contextOf(spec.Args)
  if resident == 0 || spec.Stream.Context <= resident {
    return nil
  }

  return &Window{ m: m, name: name, resident: resident, max: spec.Stream.Context }
}

func (w *Window) Resident() int { return w.resident }

func (w *Window) Max() int      { return w.max }

// Streaming reports whether the large tier is active.
func (w *Window) Streaming() bool {
  w.m.mu.Lock()
  defer w.m.mu.Unlock()

  return w.m.tiers[w.name] == TierStream
}

// Size is the active tier's window.
func (w *Window) Size() int {
  if w.Streaming() {
    return w.max
  }

  return w.resident
}

// Grow moves to the stream tier and returns its window.
func (w *Window) Grow(ctx context.Context) (int, error) {
  err := w.m.SetTier(ctx, w.name, TierStream)
  if err != nil {
    return 0, err
  }

  return w.max, nil
}

// Shrink moves back to the resident tier and returns its window.
func (w *Window) Shrink(ctx context.Context) (int, error) {
  err := w.m.SetTier(ctx, w.name, TierResident)
  if err != nil {
    return 0, err
  }

  return w.resident, nil
}
