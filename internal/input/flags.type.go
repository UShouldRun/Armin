// The parsed command line, before Parse turns it into Options.

package input

import (
  "time"
)

// cliFlags holds every parsed flag — one struct instead of a
// scattering of *string/*bool locals, so run() reads its config
// from one place.
type cliFlags struct {
  root, model, completionModel, ctagsBin    string
  webAddr, addDirs                          string
  llamaURL, completionLlamaURL              string
  timeout                                   time.Duration
  maxOutput, maxRounds                      int
  resume                                    string // "", "menu" or an id
  dryRun, mcpMode, lspMode, noSave, noColor bool
  autoApprove, newInstance                  bool
  librarySync                               bool
  libraryAdd, libraryTo, effort, aids       string
  install, installTo, uninstall             string
}

// resumeFlag is -resume: a bool flag that can also carry a value.
// "-resume" alone means "show the menu"; "-resume=<id>" (or
// "-resume <id>", see extractPositionalRoot) names a session.
type resumeFlag string
