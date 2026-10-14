package think

import (
  "context"
  "strings"
  "os"
  "time"

  "armin/internal/catalog"
  "armin/internal/config"
  "armin/internal/input"
  "armin/internal/llamacpp"
  "armin/internal/llm"
)

// remoteSpec builds the cloud-machine part of a model, if it has
// one, filling defaults: the Ubuntu image user, llama-server's
// default port, and the usual region variables.
func remoteSpec(c config.ModelSpecConfig) *llamacpp.RemoteSpec {
  r := &llamacpp.RemoteSpec{
    SSHUser:          firstNonEmpty(c.SSHUser, "ubuntu"),
    SSHKey:           c.SSHKey,
    RemotePort:       firstNonZeroInt(c.RemotePort, 8080),
    StopOnDisconnect: !c.KeepOnDisconnect,
  }

  switch {
  case c.AWSInstance != "":
    r.Provider, r.MachineID = llamacpp.ProviderAWS, c.AWSInstance
    r.Region = firstNonEmpty(
      c.AWSRegion,
      firstNonEmpty(os.Getenv("AWS_REGION"), os.Getenv("AWS_DEFAULT_REGION")),
    )
  case c.StackitServer != "":
    r.Provider, r.MachineID = llamacpp.ProviderSTACKIT, c.StackitServer
    r.ProjectID = firstNonEmpty(c.StackitProject, os.Getenv("STACKIT_PROJECT_ID"))
    r.Region = firstNonEmpty(
      c.StackitRegion, firstNonEmpty(os.Getenv("STACKIT_REGION"), "eu01"),
    )
  case c.ScalewayServer != "":
    r.Provider, r.MachineID = llamacpp.ProviderScaleway, c.ScalewayServer
    r.Region = firstNonEmpty(
      c.ScalewayZone, firstNonEmpty(os.Getenv("SCW_DEFAULT_ZONE"), "fr-par-2"),
    )
    // Scaleway's GPU images log in as root.
    r.SSHUser = firstNonEmpty(c.SSHUser, "root")
  default:
    return nil
  }

  return r
}

// queryContextSize asks the server for its window, briefly — a
// remote endpoint without /props just leaves auto-compaction off.
func queryContextSize(ctx context.Context, client *llm.Client) int {
  ctx, cancel := context.WithTimeout(ctx, 5 * time.Second)
  defer cancel()
  n, err := client.ContextSize(ctx)
  if err != nil {
    return 0
  }

  return n
}

// buildModelSpecs merges config.conf's models.<name> blocks with
// the zero-config fallback: a name with no matching Models entry
// (the common case for someone who just started a bare llama-server
// by hand) becomes an external, unmanaged spec at -llama-url /
// -completion-llama-url — so "point armin at a server you already
// run" keeps working without writing a config file at all, and
// writing models.<name>.binary/model_path entries is only needed
// once you want armin to spawn and reap the process itself.
func buildModelSpecs(opts *input.Options) map[string]llamacpp.ModelSpec {
  fileCfg := opts.Config
  specs := make(map[string]llamacpp.ModelSpec, len(fileCfg.Models) + 2)
  for name, c := range fileCfg.Models {
    spec := llamacpp.ModelSpec{
      URL: catalog.Regional(c.URL), APIKeyEnv: c.APIKeyEnv, ID: c.ID, Effort: c.Reasoning,
      Binary: c.Binary, ModelPath: c.ModelPath,
      Port: c.Port, Args: c.Args, IdleTimeout: c.IdleTimeout,
    }

    if spec.URL != c.URL {
      spec.Flags |= llamacpp.SpecRegional
    }

    if c.Vision {
      spec.Flags |= llamacpp.SpecVision
    }

    if c.NoLean {
      spec.Flags |= llamacpp.SpecNoLean
    }

    if c.StreamContext > 0 {
      spec.Stream = &llamacpp.StreamSpec{
        Binary:   c.StreamBinary,
        Context:  c.StreamContext,
        ArenaMiB: c.StreamArenaMiB,
        Args:     c.StreamArgs,
      }
    }

    r := remoteSpec(c)
    if r != nil {
      spec.Remote = r
      spec.Binary = firstNonEmpty(spec.Binary, "llama-server")
      // 18180, not 18080: that one is the local SearXNG container's.
      spec.Port = firstNonZeroInt(spec.Port, 18180)
    }

    specs[name] = spec
  }

  // "planner:<id>" is planner's endpoint with another id (see
  // llamacpp.Manager.adopt), not a server to fall back to.
  _, ok := specs[opts.Model]
  if !ok && !hostedID(specs, opts.Model) {
    specs[opts.Model] = llamacpp.ModelSpec{ URL: opts.LlamaURL }
  }

  _, ok = specs[opts.CompletionModel]
  if !ok {
    url := firstNonEmpty(opts.CompletionLlamaURL, opts.LlamaURL)
    specs[opts.CompletionModel] = llamacpp.ModelSpec{ URL: url }
  }

  return specs
}

func firstNonEmpty(a, b string) string {
  if a != "" {
    return a
  }

  return b
}

func firstNonZeroInt(a, b int) int {
  if a != 0 {
    return a
  }

  return b
}

// hostedID reports whether name is "base:id" for a hosted base.
func hostedID(specs map[string]llamacpp.ModelSpec, name string) bool {
  base, id, ok := strings.Cut(name, ":")
  spec, known := specs[base]

  return ok && id != "" && known && spec.URL != ""
}
