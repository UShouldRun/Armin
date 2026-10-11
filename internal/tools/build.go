package tools

import (
  "bufio"
  "context"
  "encoding/json"
  "fmt"
  "os"
  "path/filepath"
  "strings"

  "armin/internal/sandbox"
)

// Command is one project command. Args a caller passes replace
// DefaultArgs (e.g. "./..." for go test); Sep goes between Argv and
// caller args when there are any ("npm run test -- -t foo").
type Command struct {
  Argv, DefaultArgs []string
  Sep               string
  Source            string // where it came from, for the model
}

func (c Command) set() bool { return len(c.Argv) > 0 }

func (c Command) argv(args []string) []string {
  out := append([]string{}, c.Argv...)
  switch {
  case len(args) == 0:
    out = append(out, c.DefaultArgs...)
  case c.Sep != "":
    out = append(append(out, c.Sep), args...)
  default:
    out = append(out, args...)
  }

  return out
}

// ProjectCommands is the resolved test/build/lint trio for a root.
type ProjectCommands struct {
  Test, Build, Lint Command
}

// projectConfFile lets a repo pin its own commands. It runs repo-
// controlled code — but so does "make test" or "npm test", which
// detection would pick anyway.
const projectConfFile = ".armin/commands.conf"

// DetectCommands resolves each command in order: the user's config
// (cfg.Commands), the project's .armin/commands.conf, a Makefile
// target, then the language toolchain the manifests point to.
// Detection is a few stats per call — cheap enough to redo every
// time, so a manifest created mid-session is picked up.
func DetectCommands(cfg *sandbox.Config) ProjectCommands {
  var pc ProjectCommands

  user := map[string][]string{
    "test":  cfg.Commands.Test,
    "build": cfg.Commands.Build,
    "lint":  cfg.Commands.Lint,
  }

  project := readCommandsConf(filepath.Join(cfg.Root, projectConfFile))
  makeTargets := makefileTargets(cfg.Root)
  lang := detectLanguage(cfg.Root)

  slots := []struct {
    name string
    dst  *Command
  }{
    { "test", &pc.Test },
    { "build", &pc.Build },
    { "lint", &pc.Lint },
  }

  for _, s := range slots {
    switch {
    case len(user[s.name]) > 0:
      *s.dst = Command{ Argv: user[s.name], Source: "config.conf" }
    case len(project[s.name]) > 0:
      *s.dst = Command{ Argv: project[s.name], Source: projectConfFile }
    case makeTargets[s.name]:
      *s.dst = Command{ Argv: []string{ "make", s.name }, Source: "Makefile" }
    default:
      *s.dst = lang[s.name]
    }
  }

  return pc
}

func readCommandsConf(path string) map[string][]string {
  out := map[string][]string{}

  f, err := os.Open(path)
  if err != nil {
    return out
  }

  defer f.Close()

  scanner := bufio.NewScanner(f)
  for scanner.Scan() {
    line := strings.TrimSpace(scanner.Text())
    if line == "" || strings.HasPrefix(line, "#") {
      continue
    }

    key, value, ok := strings.Cut(line, "=")
    if !ok {
      continue
    }

    key = strings.TrimSuffix(strings.TrimSpace(key), "_cmd")
    out[key] = strings.Fields(value)
  }

  return out
}

func makefileTargets(root string) map[string]bool {
  out := map[string]bool{}

  f, err := os.Open(filepath.Join(root, "Makefile"))
  if err != nil {
    return out
  }

  defer f.Close()

  scanner := bufio.NewScanner(f)
  for scanner.Scan() {
    target, _, ok := strings.Cut(scanner.Text(), ":")
    if !ok || strings.ContainsAny(target, " \t=$") {
      continue
    }

    switch target {
    case "test", "build", "lint":
      out[target] = true
    }
  }

  return out
}

func exists(root, name string) bool {
  _, err := os.Stat(filepath.Join(root, name))
  return err == nil
}

func detectLanguage(root string) map[string]Command {
  switch {
  case exists(root, "go.mod"):
    lint := Command{
      Argv: []string{ "go", "vet" }, DefaultArgs: []string{ "./..." },
    }

    if onPath("golangci-lint") {
      lint = Command{
        Argv:        []string{ "golangci-lint", "run" },
        DefaultArgs: []string{ "./..." },
      }
    }

    lint.Source = "go.mod"

    return map[string]Command{
      "test": {
        Argv: []string{ "go", "test" }, DefaultArgs: []string{ "./..." },
        Source: "go.mod",
      },
      "build": {
        Argv: []string{ "go", "build" }, DefaultArgs: []string{ "./..." },
        Source: "go.mod",
      },
      "lint": lint,
    }

  case exists(root, "Cargo.toml"):
    return map[string]Command{
      "test":  { Argv: []string{ "cargo", "test" }, Source: "Cargo.toml" },
      "build": { Argv: []string{ "cargo", "build" }, Source: "Cargo.toml" },
      "lint":  { Argv: []string{ "cargo", "clippy" }, Source: "Cargo.toml" },
    }

  case exists(root, "package.json"):
    return nodeCommands(root)

  case exists(root, "pyproject.toml"), exists(root, "setup.py"),
    exists(root, "setup.cfg"), exists(root, "pytest.ini"),
    exists(root, "requirements.txt"):
    out := map[string]Command{
      "test": {
        Argv:   []string{ "python3", "-m", "pytest", "-q" },
        Source: "python",
      },
      "build": {
        Argv:        []string{ "python3", "-m", "compileall", "-q" },
        DefaultArgs: []string{ "." },
        Source:      "python",
      },
    }

    if onPath("ruff") {
      out["lint"] = Command{
        Argv: []string{ "ruff", "check" }, DefaultArgs: []string{ "." },
        Source: "python",
      }
    }

    return out
  }

  return nil
}

func nodeCommands(root string) map[string]Command {
  pm := "npm"
  switch {
  case exists(root, "pnpm-lock.yaml"):
    pm = "pnpm"
  case exists(root, "yarn.lock"):
    pm = "yarn"
  case exists(root, "bun.lockb"), exists(root, "bun.lock"):
    pm = "bun"
  }

  sep := ""
  if pm == "npm" {
    sep = "--"
  }

  var manifest struct {
    Scripts map[string]string `json:"scripts"`
  }

  raw, _ := os.ReadFile(filepath.Join(root, "package.json"))
  json.Unmarshal(raw, &manifest)

  out := map[string]Command{}
  for _, name := range []string{ "test", "build", "lint" } {
    if manifest.Scripts[name] != "" {
      out[name] = Command{
        Argv:   []string{ pm, "run", name },
        Sep:    sep,
        Source: "package.json",
      }
    }
  }

  if !out["build"].set() && exists(root, "tsconfig.json") {
    out["build"] = Command{
      Argv:   []string{ "npx", "tsc", "--noEmit" },
      Source: "tsconfig.json",
    }
  }

  return out
}

// ProjectRunOutput is the shared result of run_tests/build_check/
// lint_check.
type ProjectRunOutput struct {
  Command   string `json:"command,omitempty"`
  Source    string `json:"source,omitempty"`
  Summary   string `json:"summary,omitempty"`
  Output    string `json:"output,omitempty"`
  Hint      string `json:"hint,omitempty"`
  Available bool   `json:"available"`
  Passed    bool   `json:"passed"`
  Truncated bool   `json:"truncated,omitempty"`
}

// ProjectRunInput is the argument set all three tools share.
// Package is the old Go-only argument, kept as a one-arg alias.
type ProjectRunInput struct {
  Args    []string `json:"args,omitempty"`
  Package string   `json:"package,omitempty"`
}

type (
  RunTestsInput   = ProjectRunInput
  BuildCheckInput = ProjectRunInput
  LintCheckInput  = ProjectRunInput
)

func runProject(
  ctx context.Context, cfg *sandbox.Config,
  kind string, cmd Command, in ProjectRunInput,
) (*ProjectRunOutput, error) {
  if !cmd.set() {
    return &ProjectRunOutput{
      Hint: fmt.Sprintf(
        "no %s command detected for this project — the user can "+
          "set %s_cmd in %s or config.conf",
        kind, kind, projectConfFile,
      ),
    }, nil
  }

  args := in.Args
  if len(args) == 0 && in.Package != "" {
    args = []string{ in.Package }
  }

  argv := cmd.argv(args)

  if !onPath(argv[0]) {
    return &ProjectRunOutput{
      Command: strings.Join(argv, " "),
      Source:  cmd.Source,
      Hint:    fmt.Sprintf("%q is not installed (not on PATH)", argv[0]),
    }, nil
  }

  res, err := cfg.RunBuild(ctx, argv[0], argv[1:])
  if err != nil {
    return nil, err
  }

  output := res.Stdout + res.Stderr
  if output == "" && res.ExitCode == 0 {
    output = "(no output)"
  }

  summary := failureLines(output)
  if argv[0] == "go" {
    summary = summarizeGoOutput(output)
  }

  tail, truncated := sandbox.TruncateTail(output, cfg.MaxOutput)

  return &ProjectRunOutput{
    Available: true,
    Command:   strings.Join(argv, " "),
    Source:    cmd.Source,
    Passed:    res.ExitCode == 0,
    Summary:   summary,
    Output:    tail,
    Truncated: truncated,
  }, nil
}

// RunTests runs the project's test command.
func RunTests(
  ctx context.Context, cfg *sandbox.Config, in RunTestsInput,
) (*ProjectRunOutput, error) {
  out, err := runProject(ctx, cfg, "test", DetectCommands(cfg).Test, in)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in RunTests:\n\t%w", err)
  }

  return out, nil
}

// BuildCheck runs the project's build (or type-check) command.
func BuildCheck(
  ctx context.Context, cfg *sandbox.Config, in BuildCheckInput,
) (*ProjectRunOutput, error) {
  out, err := runProject(ctx, cfg, "build", DetectCommands(cfg).Build, in)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in BuildCheck:\n\t%w", err)
  }

  return out, nil
}

// LintCheck runs the project's lint command. No linter is reported
// as available:false, not an error.
func LintCheck(
  ctx context.Context, cfg *sandbox.Config, in LintCheckInput,
) (*ProjectRunOutput, error) {
  out, err := runProject(ctx, cfg, "lint", DetectCommands(cfg).Lint, in)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in LintCheck:\n\t%w", err)
  }

  return out, nil
}

// summarizeGoOutput keeps only go test/build's own "ok"/"FAIL"/
// "---"/"#" result lines — the signal in otherwise-verbose output.
func summarizeGoOutput(output string) string {
  var lines []string
  for _, line := range strings.Split(output, "\n") {
    trimmed := strings.TrimSpace(line)
    isResultLine := strings.HasPrefix(trimmed, "ok ") ||
      strings.HasPrefix(trimmed, "FAIL") ||
      strings.HasPrefix(trimmed, "---") ||
      strings.HasPrefix(trimmed, "# ")
    if isResultLine {
      lines = append(lines, trimmed)
    }
  }

  if len(lines) == 0 {
    return "(no per-package result lines found)"
  }

  return strings.Join(lines, "\n")
}

// maxSummaryLines caps failureLines.
const maxSummaryLines = 25

// failureLines is the language-agnostic summary: lines that look
// like a failure, an error, or a final verdict.
func failureLines(output string) string {
  markers := []string{
    "FAIL", "fail", "Error", "error", "panic", "✗", "✕",
    "passed", "failed", "Traceback",
  }

  var lines []string
  for _, line := range strings.Split(output, "\n") {
    trimmed := strings.TrimSpace(line)
    for _, m := range markers {
      if strings.Contains(trimmed, m) {
        lines = append(lines, trimmed)
        break
      }
    }

    if len(lines) == maxSummaryLines {
      break
    }
  }

  return strings.Join(lines, "\n")
}
