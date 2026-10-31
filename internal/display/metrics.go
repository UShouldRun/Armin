// -metrics: what armin's deterministic systems did across every run,
// from <data>/armin/metrics (see internal/metrics).

package display

import (
  "fmt"
  "os"
  "path/filepath"

  "armin/internal/metrics"
  "armin/internal/session"
)

// Metrics prints the report and exits.
func Metrics() error {
  data, err := session.DataDir()
  if err != nil {
    return fmt.Errorf("\n\t... in Metrics:\n\t%w", err)
  }

  dir := filepath.Join(data, "metrics")
  err = metrics.Report(os.Stdout, dir)
  if err != nil {
    return fmt.Errorf("\n\t... in Metrics:\n\t%w", err)
  }

  if !metrics.Debug {
    fmt.Println("\ncounts only; build with -tags debug for every event in", dir)
  }

  return nil
}
