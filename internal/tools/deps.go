// Dependencies: ListDependencies parses whichever manifest exists,
// ModuleGraph shows how the project's own packages import each other.

package tools

import (
  "bufio"
  "context"
  "encoding/json"
  "fmt"
  "os"
  "sort"
  "strings"

  "armin/internal/sandbox"
)

// manifestParsers is checked in order; the first manifest file
// found at the project root wins. Each parser is a minimal,
// single-purpose reader, not a general-purpose format library.
var manifestParsers = []struct {
  file  string
  parse func([]byte) ([]Dependency, error)
}{
  { "go.mod", parseGoMod },
  { "package.json", parsePackageJSON },
  { "requirements.txt", parseRequirementsTxt },
  { "Cargo.toml", parseCargoToml },
  { "pyproject.toml", parsePyprojectToml },
}

// ListDependencies detects and parses whichever dependency manifest
// exists at the project root. go.mod and package.json get a real
// structured parse (stdlib-sufficient for both); Cargo.toml and
// pyproject.toml get a minimal hand-rolled [section] + key = "value"
// scanner good enough for a flat dependencies table, not full TOML
// compliance — arrays, inline tables, and multi-line strings aren't
// handled. armin takes on no TOML/YAML dependency for this.
func ListDependencies(
  ctx context.Context, cfg *sandbox.Config, in ListDependenciesInput,
) (*ListDependenciesOutput, error) {
  for _, p := range manifestParsers {
    resolved, err := cfg.ResolvePath(p.file)
    if err != nil {
      continue
    }
    data, err := os.ReadFile(resolved)
    if err != nil {
      continue
    }
    deps, err := p.parse(data)
    if err != nil {
      return nil, fmt.Errorf(
        "\n\t... in ListDependencies:\n\t parsing %s: %w", p.file, err,
      )
    }
    return &ListDependenciesOutput{ Manifest: p.file, Dependencies: deps }, nil
  }
  return nil, fmt.Errorf(
    "\n\t... in ListDependencies:\n\t no recognized manifest " +
      "(go.mod, package.json, requirements.txt, Cargo.toml, " +
      "pyproject.toml) found at the project root",
  )
}

func parseGoMod(data []byte) ([]Dependency, error) {
  var deps []Dependency
  inBlock := false

  scanner := bufio.NewScanner(strings.NewReader(string(data)))
  for scanner.Scan() {
    line := strings.TrimSpace(scanner.Text())
    switch {
    case line == "require (":
      inBlock = true
    case inBlock && line == ")":
      inBlock = false
    case inBlock || strings.HasPrefix(line, "require "):
      line = strings.TrimPrefix(line, "require ")
      line = strings.TrimSuffix(line, " // indirect")
      fields := strings.Fields(line)
      if len(fields) >= 2 {
        deps = append(deps, Dependency{ Name: fields[0], Version: fields[1] })
      }
    }
  }
  return deps, scanner.Err()
}

func parsePackageJSON(data []byte) ([]Dependency, error) {
  var pkg struct {
    Dependencies    map[string]string `json:"dependencies"`
    DevDependencies map[string]string `json:"devDependencies"`
  }
  err := json.Unmarshal(data, &pkg)
  if err != nil {
    return nil, err
  }

  var deps []Dependency
  for name, ver := range pkg.Dependencies {
    deps = append(deps, Dependency{ Name: name, Version: ver })
  }
  for name, ver := range pkg.DevDependencies {
    deps = append(deps, Dependency{ Name: name, Version: ver + " (dev)" })
  }
  sort.Slice(deps, func(i, j int) bool { return deps[i].Name < deps[j].Name })
  return deps, nil
}

func parseRequirementsTxt(data []byte) ([]Dependency, error) {
  var deps []Dependency
  seps := []string{ "==", ">=", "<=", "~=", ">", "<" }

  scanner := bufio.NewScanner(strings.NewReader(string(data)))
  for scanner.Scan() {
    line := strings.TrimSpace(scanner.Text())
    if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
      continue
    }
    name, version, found := cutAny(line, seps)
    if !found {
      deps = append(deps, Dependency{ Name: line })
      continue
    }
    deps = append(deps, Dependency{
      Name: strings.TrimSpace(name), Version: strings.TrimSpace(version),
    })
  }
  return deps, scanner.Err()
}

func cutAny(s string, seps []string) (before, after string, found bool) {
  for _, sep := range seps {
    i := strings.Index(s, sep)
    if i >= 0 {
      return s[:i], s[i:], true
    }
  }
  return s, "", false
}

// parseTomlDepsSection is the shared minimal scanner behind
// parseCargoToml/parsePyprojectToml: finds a named [section] header
// and reads flat `key = "value"` lines until the next [section] or
// EOF. It doesn't handle nested tables, arrays, or multi-line
// strings — see ListDependencies' doc comment.
func parseTomlDepsSection(data []byte, section string) []Dependency {
  var deps []Dependency
  inSection := false

  scanner := bufio.NewScanner(strings.NewReader(string(data)))
  for scanner.Scan() {
    line := strings.TrimSpace(scanner.Text())
    if strings.HasPrefix(line, "[") {
      inSection = line == section
      continue
    }
    if !inSection || line == "" || strings.HasPrefix(line, "#") {
      continue
    }
    key, value, ok := strings.Cut(line, "=")
    if !ok {
      continue
    }
    deps = append(deps, Dependency{
      Name:    strings.TrimSpace(key),
      Version: strings.Trim(strings.TrimSpace(value), `"'`),
    })
  }
  return deps
}

func parseCargoToml(data []byte) ([]Dependency, error) {
  return parseTomlDepsSection(data, "[dependencies]"), nil
}

func parsePyprojectToml(data []byte) ([]Dependency, error) {
  deps := parseTomlDepsSection(data, "[tool.poetry.dependencies]")
  if len(deps) == 0 {
    deps = parseTomlDepsSection(data, "[project.dependencies]")
  }
  return deps, nil
}

// --- module_graph ---

// ModuleGraph shows which of this module's own packages import
// which, via `go list -json` (the stdlib go tool — no
// golang.org/x/tools/go/packages dependency), filtered down to just
// this module's own import path prefix. Deliberately not `-deps`:
// that pulls in every transitively-reached stdlib/third-party
// package's full JSON metadata too — easily large enough on a real
// module to blow past sandbox.Config's MaxOutput and truncate mid-
// stream, corrupting the JSON. parseModuleGraph only ever reads each
// package's own (already-direct) Imports field, so the transitive
// closure -deps adds was never actually used.
func ModuleGraph(
  ctx context.Context, cfg *sandbox.Config, in ModuleGraphInput,
) (*ModuleGraphOutput, error) {
  modPath, err := readModulePath(cfg)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ModuleGraph:\n\t%w", err)
  }

  res, err := cfg.Exec(ctx, sandbox.Command{ Binary: "go", Args: []string{ "list", "-json", "./..." }, Mode: sandbox.ExecForce | sandbox.ExecFull })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ModuleGraph:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in ModuleGraph:\n\t go list failed: %s", firstLine(res.Stderr),
    )
  }

  imports, err := parseModuleGraph(res.Stdout, modPath)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ModuleGraph:\n\t%w", err)
  }
  return &ModuleGraphOutput{ Module: modPath, Imports: imports }, nil
}

func readModulePath(cfg *sandbox.Config) (string, error) {
  resolved, err := cfg.ResolvePath("go.mod")
  if err != nil {
    return "", fmt.Errorf("no go.mod at the project root: %w", err)
  }
  data, err := os.ReadFile(resolved)
  if err != nil {
    return "", err
  }
  for _, line := range strings.Split(string(data), "\n") {
    line = strings.TrimSpace(line)
    after, ok := strings.CutPrefix(line, "module ")
    if ok {
      return strings.TrimSpace(after), nil
    }
  }
  return "", fmt.Errorf("no module directive found in go.mod")
}

// parseModuleGraph reads `go list -deps -json`'s stream of
// concatenated JSON objects (one per package, not a JSON array) and
// keeps only edges where both the package and its import are under
// modPath — third-party and stdlib imports are noise for an
// "internal architecture" question.
func parseModuleGraph(output, modPath string) (map[string][]string, error) {
  imports := map[string][]string{}
  dec := json.NewDecoder(strings.NewReader(output))

  for dec.More() {
    var pkg struct {
      ImportPath string   `json:"ImportPath"`
      Imports    []string `json:"Imports"`
    }
    err := dec.Decode(&pkg)
    if err != nil {
      return nil, err
    }
    if !strings.HasPrefix(pkg.ImportPath, modPath) {
      continue
    }

    var internal []string
    for _, imp := range pkg.Imports {
      if strings.HasPrefix(imp, modPath) {
        internal = append(internal, imp)
      }
    }
    sort.Strings(internal)
    imports[pkg.ImportPath] = internal
  }
  return imports, nil
}
