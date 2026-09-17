package tools

import (
  "context"
  "fmt"

  "armin/internal/sandbox"
)

// SearchCodeInput is the structured argument set for search_code — a
// pattern and structured filters, never a free-form shell string.
type SearchCodeInput struct {
  Pattern  string `json:"pattern"`
  Path     string `json:"path,omitempty"`      // default: project root
  FileType string `json:"file_type,omitempty"` // e.g. "go" — passed to rg -t
}

type SearchCodeOutput struct {
  Matches   string `json:"matches"`
  Truncated bool   `json:"truncated"`
}

// SearchCode wraps ripgrep with a small, fixed set of flags: line
// numbers and a couple lines of context, never a dump of full files.
// This always actually runs — it's a fixed, trusted, read-only
// binary, not the arbitrary escape hatch --dry-run is meant to gate
// (see run_command).
func SearchCode(
  ctx context.Context, cfg *sandbox.Config, in SearchCodeInput,
) (*SearchCodeOutput, error) {
  if in.Pattern == "" {
    return nil, fmt.Errorf(
      "\n\t... in SearchCode:\n\t pattern is required",
    )
  }

  searchPath, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in SearchCode:\n\t%w", err)
  }

  args := []string{
    "--line-number",
    "--context", "2",
    "--max-columns", "300",
  }
  if in.FileType != "" {
    args = append(args, "--type", in.FileType)
  }
  args = append(args, in.Pattern, searchPath)

  res, err := cfg.RunForce(ctx, "rg", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in SearchCode:\n\t%w", err)
  }

  // rg exits 1 when there are simply no matches — that's not an error
  // worth surfacing as a tool failure, just an empty result.
  if res.ExitCode == 1 && res.Stdout == "" {
    return &SearchCodeOutput{Matches: "(no matches)"}, nil
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
