package notes

import (
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"
)

const (
  ScopeProject Scope = 1 << iota // facts about this codebase
  ScopeUser                      // the user's preferences, any project
  ScopeAll     = ScopeProject | ScopeUser
)

func (s Scope) String() string {
  if s == ScopeUser {
    return "user"
  }

  return "project"
}

// ParseScope maps the tool argument; empty means project.
func ParseScope(s string) (Scope, error) {
  switch strings.ToLower(strings.TrimSpace(s)) {
  case "", "project":
    return ScopeProject, nil
  case "user", "global":
    return ScopeUser, nil
  }

  return 0, fmt.Errorf("scope must be \"project\" or \"user\", got %q", s)
}

func (m *Memories) dir(s Scope) string {
  if s == ScopeUser {
    return m.UserDir
  }

  return m.ProjectDir
}

// List returns every memory, project scope first, each sorted by
// name.
func (m *Memories) List() ([]Memory, error) {
  var out []Memory
  for _, s := range []Scope{ ScopeProject, ScopeUser } {
    entries, err := os.ReadDir(m.dir(s))
    if os.IsNotExist(err) {
      continue
    }

    if err != nil {
      return nil, fmt.Errorf("\n\t... in List:\n\t%w", err)
    }

    start := len(out)
    for _, e := range entries {
      if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
        continue
      }

      path := filepath.Join(m.dir(s), e.Name())
      h, err := readHeader(path)
      if err != nil {
        continue
      }

      out = append(out, Memory{
        Name:        strings.TrimSuffix(e.Name(), ".md"),
        Description: h["description"],
        Scope:       s,
        Path:        path,
      })
    }

    part := out[start:]
    sort.Slice(part, func(i, j int) bool {
      return part[i].Name < part[j].Name
    })
  }

  return out, nil
}

// find looks the name up in project scope, then user scope.
func (m *Memories) find(name string) (Memory, error) {
  safe, err := SafeName(name)
  if err != nil {
    return Memory{}, err
  }

  for _, s := range []Scope{ ScopeProject, ScopeUser } {
    path := filepath.Join(m.dir(s), safe + ".md")
    _, err := os.Stat(path)
    if err == nil {
      return Memory{ Name: safe, Scope: s, Path: path }, nil
    }
  }

  return Memory{}, fmt.Errorf("no memory named %q", safe)
}

// Read returns a memory's description and body.
func (m *Memories) Read(name string) (Memory, string, error) {
  mem, err := m.find(name)
  if err != nil {
    return Memory{}, "", fmt.Errorf("\n\t... in Read:\n\t%w", err)
  }

  raw, err := os.ReadFile(mem.Path)
  if err != nil {
    return Memory{}, "", fmt.Errorf("\n\t... in Read:\n\t%w", err)
  }

  h, body := parseFile(string(raw))
  mem.Description = h["description"]

  return mem, body, nil
}

// Save creates or replaces a memory. Saving to one scope removes a
// same-named memory from the other, so a name means one thing.
func (m *Memories) Save(
  name, description, body string, scope Scope,
) (Memory, error) {
  safe, err := SafeName(name)
  if err != nil {
    return Memory{}, fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  description = oneLine(description)
  if description == "" {
    return Memory{}, fmt.Errorf(
      "\n\t... in Save:\n\t description is required — it's what " +
        "the index shows future sessions",
    )
  }

  path := filepath.Join(m.dir(scope), safe + ".md")
  content := fmt.Sprintf(
    "---\nname: %s\ndescription: %s\n---\n\n%s\n",
    safe, description, strings.TrimSpace(body),
  )
  err = writeAtomic(path, []byte(content))
  if err != nil {
    return Memory{}, fmt.Errorf("\n\t... in Save:\n\t%w", err)
  }

  other := ScopeUser
  if scope == ScopeUser {
    other = ScopeProject
  }

  os.Remove(filepath.Join(m.dir(other), safe + ".md"))

  return Memory{
    Name: safe, Description: description, Scope: scope, Path: path,
  }, nil
}

// Delete removes a memory from whichever scope holds it.
func (m *Memories) Delete(name string) (Memory, error) {
  mem, err := m.find(name)
  if err != nil {
    return Memory{}, fmt.Errorf("\n\t... in Delete:\n\t%w", err)
  }

  err = os.Remove(mem.Path)
  if err != nil {
    return Memory{}, fmt.Errorf("\n\t... in Delete:\n\t%w", err)
  }

  return mem, nil
}

// maxIndexEntries keeps the prompt index bounded however much
// accumulates; the rest stay reachable by name.
const maxIndexEntries = 60

// Index renders the memory list for the system prompt: one line per
// memory, names and descriptions only.
func (m *Memories) Index() string {
  list, err := m.List()
  if err != nil || len(list) == 0 {
    return ""
  }

  var b strings.Builder
  for i, mem := range list {
    if i == maxIndexEntries {
      fmt.Fprintf(&b, "- … %d more\n", len(list) - i)
      break
    }

    fmt.Fprintf(
      &b, "- %s (%s): %s\n", mem.Name, mem.Scope, mem.Description,
    )
  }

  return b.String()
}
