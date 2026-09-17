package tools

import (
  "context"
  "testing"

  "armin/internal/sandbox"
)

// --- parser unit tests ---

func TestParseGoMod(t *testing.T) {
  data := []byte(`module example.com/foo

go 1.23

require (
	golang.org/x/sys v0.35.0
	golang.org/x/text v0.14.0 // indirect
)
`)
  deps, err := parseGoMod(data)
  if err != nil {
    t.Fatalf("parseGoMod: %v", err)
  }
  if len(deps) != 2 {
    t.Fatalf("deps = %+v, want 2", deps)
  }
  if deps[0].Name != "golang.org/x/sys" || deps[0].Version != "v0.35.0" {
    t.Fatalf("deps[0] = %+v", deps[0])
  }
  if deps[1].Name != "golang.org/x/text" || deps[1].Version != "v0.14.0" {
    t.Fatalf("deps[1] = %+v (indirect suffix should be stripped)", deps[1])
  }
}

func TestParseGoModSingleLineRequire(t *testing.T) {
  data := []byte("module example.com/foo\n\ngo 1.23\n\nrequire golang.org/x/sys v0.35.0\n")
  deps, err := parseGoMod(data)
  if err != nil {
    t.Fatalf("parseGoMod: %v", err)
  }
  if len(deps) != 1 || deps[0].Name != "golang.org/x/sys" {
    t.Fatalf("deps = %+v", deps)
  }
}

func TestParsePackageJSON(t *testing.T) {
  data := []byte(`{
    "dependencies": { "react": "^18.0.0" },
    "devDependencies": { "typescript": "^5.0.0" }
  }`)
  deps, err := parsePackageJSON(data)
  if err != nil {
    t.Fatalf("parsePackageJSON: %v", err)
  }
  if len(deps) != 2 {
    t.Fatalf("deps = %+v, want 2", deps)
  }
  byName := map[string]Dependency{}
  for _, d := range deps {
    byName[d.Name] = d
  }
  if byName["react"].Version != "^18.0.0" {
    t.Fatalf("react = %+v", byName["react"])
  }
  if byName["typescript"].Version != "^5.0.0 (dev)" {
    t.Fatalf("typescript = %+v", byName["typescript"])
  }
}

func TestParseRequirementsTxt(t *testing.T) {
  data := []byte("# a comment\nrequests==2.31.0\nnumpy>=1.20\nflask\n")
  deps, err := parseRequirementsTxt(data)
  if err != nil {
    t.Fatalf("parseRequirementsTxt: %v", err)
  }
  if len(deps) != 3 {
    t.Fatalf("deps = %+v, want 3", deps)
  }
  if deps[0].Name != "requests" || deps[0].Version != "==2.31.0" {
    t.Fatalf("deps[0] = %+v", deps[0])
  }
  if deps[2].Name != "flask" || deps[2].Version != "" {
    t.Fatalf("deps[2] = %+v, want a bare name with no version", deps[2])
  }
}

func TestParseCargoToml(t *testing.T) {
  data := []byte("[package]\nname = \"foo\"\n\n[dependencies]\nserde = \"1.0\"\ntokio = \"1.0\"\n\n[dev-dependencies]\nother = \"2.0\"\n")
  deps, err := parseCargoToml(data)
  if err != nil {
    t.Fatalf("parseCargoToml: %v", err)
  }
  if len(deps) != 2 {
    t.Fatalf("deps = %+v, want 2 (only [dependencies], not [dev-dependencies])", deps)
  }
}

// --- list_dependencies end-to-end ---

func TestListDependenciesFindsGoMod(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "go.mod", "module foo\n\ngo 1.23\n\nrequire golang.org/x/sys v0.35.0\n")

  out, err := ListDependencies(context.Background(), cfg, ListDependenciesInput{})
  if err != nil {
    t.Fatalf("ListDependencies: %v", err)
  }
  if out.Manifest != "go.mod" || len(out.Dependencies) != 1 {
    t.Fatalf("out = %+v", out)
  }
}

func TestListDependenciesNoManifest(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "nothing here")

  if _, err := ListDependencies(context.Background(), cfg, ListDependenciesInput{}); err == nil {
    t.Fatal("expected an error when no manifest is present")
  }
}

// --- module_graph ---

func TestModuleGraphInternalEdgesOnly(t *testing.T) {
  requireGo(t)
  cfg := newTestSandbox(t, sandbox.Options{ Timeout: 60_000_000_000 })

  writeFile(t, cfg.Root, "go.mod", "module fixture\n\ngo 1.21\n")
  writeFile(t, cfg.Root, "main.go", "package main\n\nimport \"fixture/pkg\"\n\nfunc main() { pkg.Hello() }\n")
  writeFile(t, cfg.Root, "pkg/pkg.go", "package pkg\n\nimport \"fmt\"\n\nfunc Hello() { fmt.Println(\"hi\") }\n")

  out, err := ModuleGraph(context.Background(), cfg, ModuleGraphInput{})
  if err != nil {
    t.Fatalf("ModuleGraph: %v", err)
  }
  if out.Module != "fixture" {
    t.Fatalf("Module = %q, want fixture", out.Module)
  }
  imports := out.Imports["fixture"]
  if len(imports) != 1 || imports[0] != "fixture/pkg" {
    t.Fatalf(`Imports["fixture"] = %v, want ["fixture/pkg"] (fmt is stdlib, excluded)`, imports)
  }
}
