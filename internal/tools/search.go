// SearchCode: ripgrep over the project, matches with a little context.

package tools

import (
  "context"
  "fmt"

  "armin/internal/sandbox"
)

// searchContextMax caps the lines shown around a match.
const searchContextMax = 10

// SearchCode wraps ripgrep with a small, fixed set of flags: line
// numbers and a couple lines of context (more on request), never a
// dump of full files; the pattern a regex, or literal on request.
// This always actually runs — it's a fixed, trusted, read-only
// binary, not the arbitrary escape hatch --dry-run is meant to gate
// (see run_command).
func SearchCode(
  ctx context.Context, cfg *sandbox.Config, in SearchCodeInput,
) (*SearchCodeOutput, error) {
  if in.Pattern == "" {
    return nil, fmt.Errorf("\n\t... in SearchCode:\n\t pattern is required")
  }

  searchPath, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in SearchCode:\n\t%w", err)
  }

  around := 2
  if in.Context > 0 {
    around = min(in.Context, searchContextMax)
  }

  args := []string{
    "--line-number",
    "--context", fmt.Sprint(around),
    "--max-columns", "300",
  }
  if in.FileType != "" {
    args = append(args, "--type", in.FileType)
  }

  if in.Literal {
    args = append(args, "--fixed-strings")
  }

  // "--" first: a pattern starting with "-" ("---") is a pattern,
  // not a flag
  args = append(args, "--", in.Pattern, searchPath)

  res, err := cfg.Exec(
    ctx,
    sandbox.Command{ Binary: "rg", Args: args, Mode: sandbox.ExecForce },
  )
  if err != nil {
    return nil, fmt.Errorf("\n\t... in SearchCode:\n\t%w", err)
  }

  // rg exits 1 when there are simply no matches — that's not an error
  // worth surfacing as a tool failure, just an empty result.
  if res.ExitCode == 1 && res.Stdout == "" {
    return &SearchCodeOutput{ Matches: "(no matches)" }, nil
  }

  if res.ExitCode > 1 {
    return nil, fmt.Errorf(
      "\n\t... in SearchCode:\n\t rg failed: %s", res.Stderr,
    )
  }

  return &SearchCodeOutput{
    Matches:   res.Stdout,
    Truncated: res.Flags.Truncated(),
  }, nil
}
