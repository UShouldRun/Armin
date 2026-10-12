package notes

// Scope says which memory directory an entry lives in.
type Scope uint8

// Memory is one remembered fact: a file whose frontmatter carries
// the name and a one-line description for the index.
type Memory struct {
  Name, Description string
  Scope             Scope
  Path              string
}

// Memories is the two-directory memory store.
type Memories struct {
  ProjectDir, UserDir string
}
