// Inputs and outputs of deps.go's tools.

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
  // this project's own module path, from go.mod
  Module  string              `json:"module"`
  // package path -> internal packages it imports
  Imports map[string][]string `json:"imports"`
}
