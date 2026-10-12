package input

import (
  "context"
  "os"
  "os/signal"
  "syscall"
)

// WatchSignals starts listening. Ctrl-C goes to OnInterrupt's
// handler first (stopping a turn) and cancels everything only when
// that declines; SIGTERM/SIGHUP (kill, closed terminal) cancel, run
// OnTerminate's cleanup — an EC2 instance armin started must not
// outlive it — and exit.
func WatchSignals(cancel context.CancelFunc) *Signals {
  s := &Signals{}

  term := make(chan os.Signal, 1)
  signal.Notify(term, syscall.SIGTERM, syscall.SIGHUP)
  go func() {
    <-term
    cancel()
    f := s.terminate.Load()
    if f != nil {
      (*f)()
    }

    os.Exit(1)
  }()

  stop := make(chan os.Signal, 1)
  signal.Notify(stop, os.Interrupt)
  go func() {
    for range stop {
      f := s.interrupt.Load()
      if f != nil && (*f)() {
        continue
      }

      cancel()

      return
    }
  }()

  return s
}

// OnInterrupt sets Ctrl-C's handler: true means it handled it.
func (s *Signals) OnInterrupt(f func() bool) { s.interrupt.Store(&f) }

// OnTerminate sets the cleanup run before a SIGTERM/SIGHUP exit.
func (s *Signals) OnTerminate(f func()) { s.terminate.Store(&f) }
