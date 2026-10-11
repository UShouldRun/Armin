package notes

import (
  "fmt"
  "io/fs"
  "os"
  "path/filepath"
  "sort"
  "strings"
)

// skillFile is the entry point every skill directory must have.
const skillFile = "SKILL.md"

// maxSkillFileBytes caps one file read out of a skill directory.
const maxSkillFileBytes = 64 * 1024

// Skill is a directory of instructions the model loads only when a
// task calls for it: SKILL.md (frontmatter name + description, then
// the instructions) plus any supporting files.
type Skill struct {
  Name, Description, Dir string
  Project                bool
}

// Skills is every skill found at startup, by name.
type Skills struct {
  byName map[string]Skill
  names  []string
}

// LoadSkills scans the user's skill directory, then the project's;
// a project skill replaces a user skill of the same name.
func LoadSkills(userDir, projectDir string) *Skills {
  s := &Skills{ byName: map[string]Skill{} }
  s.scan(userDir, false)
  s.scan(projectDir, true)

  for name := range s.byName {
    s.names = append(s.names, name)
  }

  sort.Strings(s.names)

  return s
}

func (s *Skills) scan(dir string, project bool) {
  entries, err := os.ReadDir(dir)
  if err != nil {
    return
  }

  for _, e := range entries {
    if !e.IsDir() {
      continue
    }

    skillDir := filepath.Join(dir, e.Name())
    h, err := readHeader(filepath.Join(skillDir, skillFile))
    if err != nil {
      continue
    }

    name, err := SafeName(firstNonEmpty(h["name"], e.Name()))
    if err != nil {
      continue
    }

    s.byName[name] = Skill{
      Name:        name,
      Description: oneLine(h["description"]),
      Dir:         skillDir,
      Project:     project,
    }
  }
}

func firstNonEmpty(a, b string) string {
  if a != "" {
    return a
  }

  return b
}

// List returns every skill sorted by name.
func (s *Skills) List() []Skill {
  out := make([]Skill, len(s.names))
  for i, n := range s.names {
    out[i] = s.byName[n]
  }

  return out
}

// Get looks a skill up by name.
func (s *Skills) Get(name string) (Skill, bool) {
  safe, err := SafeName(name)
  if err != nil {
    return Skill{}, false
  }

  sk, ok := s.byName[safe]

  return sk, ok
}

// Len is the number of skills loaded.
func (s *Skills) Len() int { return len(s.names) }

// Instructions returns SKILL.md's body, without frontmatter.
func (sk Skill) Instructions() (string, error) {
  raw, err := os.ReadFile(filepath.Join(sk.Dir, skillFile))
  if err != nil {
    return "", fmt.Errorf("\n\t... in Instructions:\n\t%w", err)
  }

  _, body := parseFile(string(raw))

  return body, nil
}

// maxSkillFiles caps the supporting-file listing.
const maxSkillFiles = 50

// Files lists the skill's supporting files, relative to its
// directory.
func (sk Skill) Files() []string {
  var out []string
  filepath.WalkDir(sk.Dir, func(path string, d fs.DirEntry, err error) error {
    if len(out) == maxSkillFiles {
      return filepath.SkipAll
    }

    if err != nil {
      return nil
    }

    if d.IsDir() {
      if strings.HasPrefix(d.Name(), ".") && path != sk.Dir {
        return filepath.SkipDir
      }

      return nil
    }

    rel, err := filepath.Rel(sk.Dir, path)
    if err == nil && rel != skillFile {
      out = append(out, filepath.ToSlash(rel))
    }

    return nil
  })

  return out
}

// ReadFile reads one supporting file, jailed to the skill directory.
func (sk Skill) ReadFile(rel string) (string, bool, error) {
  base, err := filepath.EvalSymlinks(sk.Dir)
  if err != nil {
    return "", false, fmt.Errorf("\n\t... in ReadFile:\n\t%w", err)
  }

  path, err := filepath.EvalSymlinks(filepath.Join(base, rel))
  if err != nil {
    return "", false, fmt.Errorf("\n\t... in ReadFile:\n\t%w", err)
  }

  r, err := filepath.Rel(base, path)
  if err != nil || r == ".." || strings.HasPrefix(r, "../") {
    return "", false, fmt.Errorf(
      "\n\t... in ReadFile:\n\t %q is outside the skill", rel,
    )
  }

  raw, err := os.ReadFile(path)
  if err != nil {
    return "", false, fmt.Errorf("\n\t... in ReadFile:\n\t%w", err)
  }

  if len(raw) > maxSkillFileBytes {
    return string(raw[:maxSkillFileBytes]), true, nil
  }

  return string(raw), false, nil
}

// Index renders the skill list for the system prompt.
func (s *Skills) Index() string {
  var b strings.Builder
  for _, sk := range s.List() {
    fmt.Fprintf(&b, "- %s: %s\n", sk.Name, sk.Description)
  }

  return b.String()
}
