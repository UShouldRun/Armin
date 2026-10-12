package notes

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
