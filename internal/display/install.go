// -install: size a model up against this machine, download it and
// add it to config.conf, then exit. -uninstall: take one out again.

package display

import (
  "context"
  "fmt"

  "armin/internal/input"
  "armin/internal/install"
)

// Install is -install and -uninstall. Neither needs a Mind: nothing
// is loaded.
func Install(ctx context.Context, opts *input.Options) error {
  models := make(map[string]install.Existing, len(opts.Config.Models))
  for name, m := range opts.Config.Models {
    models[name] = install.Existing{
      Binary:    m.Binary,
      ModelPath: m.ModelPath,
      Port:      m.Port,
      Remote:    m.AWSInstance != "" || m.StackitServer != "" || m.ScalewayServer != "",
      External:  m.URL != "",
    }
  }

  req := install.Request{
    Model:      opts.Install,
    To:         opts.InstallTo,
    Yes:        opts.Has(input.FlagAutoApprove),
    Models:     models,
    RAMHint:    opts.Config.RAM,
    Default:    opts.Config.Model,
    Completion: opts.Config.CompletionModel,
  }

  var err error
  if opts.Uninstall != "" {
    req.Model = opts.Uninstall
    err = install.Uninstall(req)
  } else {
    err = install.Run(ctx, req)
  }

  if err != nil {
    return fmt.Errorf("\n\t... in Install:\n\t%w", err)
  }

  return nil
}
