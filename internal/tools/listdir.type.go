package tools

// ListDirectoryInput is the argument set for list_directory.
type ListDirectoryInput struct {
  Path  string `json:"path,omitempty"`  // default: project root
  Depth int    `json:"depth,omitempty"` // default: 2
}

type ListDirectoryOutput struct {
  Tree string `json:"tree"`
}
