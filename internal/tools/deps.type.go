package tools

type Dependency struct {
  Name    string `json:"name"`
  Version string `json:"version,omitempty"`
}

type ListDependenciesInput struct{}

type ListDependenciesOutput struct {
  Manifest     string       `json:"manifest"` // which file this came from
  Dependencies []Dependency `json:"dependencies"`
}

type ModuleGraphInput struct{}

type ModuleGraphOutput struct {
  Module  string              `json:"module"`  // this project's own module path, from go.mod
  Imports map[string][]string `json:"imports"` // package path -> internal packages it imports
}
