// Package notes holds what lets the model keep less in context:
// memories (durable facts across sessions), skills (instructions
// loaded on demand) and the scratchpad (working notes for the
// current task). All three are plain Markdown files on disk; only
// short indexes and pointers ever sit in the conversation.
package notes

import (
  "bufio"
  "fmt"
  "os"
  "path/filepath"
  "strings"
)

// maxNameLen caps a memory/scratch/skill name.
const maxNameLen = 64

// SafeName lowercases name and keeps [a-z0-9-_.], so a model-chosen
// name can't walk out of its directory or collide by case.
func SafeName(name string) (string, error) {
  name = strings.TrimSuffix(strings.TrimSpace(name), ".md")

  var b strings.Builder
  for _, r := range strings.ToLower(name) {
    switch {
    case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
      b.WriteRune(r)
    case r == ' ', r == '/', r == '.':
      b.WriteRune('-')
    }
  }

  out := strings.Trim(b.String(), "-")
  switch {
  case out == "":
    return "", fmt.Errorf("name %q has no usable characters", name)
  case len(out) > maxNameLen:
    return "", fmt.Errorf("name %q is over %d characters", name, maxNameLen)
  }

  return out, nil
}

// parseFile splits a Markdown file into frontmatter and body. A
// file without frontmatter is all body.
func parseFile(raw string) (header, string) {
  h := header{}
  rest, ok := strings.CutPrefix(raw, "---\n")
  if !ok {
    return h, raw
  }

  end := strings.Index(rest, "\n---")
  if end < 0 {
    return h, raw
  }

  for _, line := range strings.Split(rest[:end], "\n") {
    key, value, ok := strings.Cut(line, ":")
    if ok {
      h[strings.TrimSpace(key)] = strings.Trim(
        strings.TrimSpace(value), `"'`,
      )
    }
  }

  body := rest[end + len("\n---"):]
  body = strings.TrimPrefix(body, "\n")

  return h, body
}

// readHeader reads only a file's frontmatter, not its body — index
// building touches every file, so it shouldn't load them whole.
func readHeader(path string) (header, error) {
  f, err := os.Open(path)
  if err != nil {
    return nil, err
  }

  defer f.Close()

  h := header{}
  scanner := bufio.NewScanner(f)
  if !scanner.Scan() || scanner.Text() != "---" {
    return h, scanner.Err()
  }

  for scanner.Scan() {
    line := scanner.Text()
    if line == "---" {
      break
    }

    key, value, ok := strings.Cut(line, ":")
    if ok {
      h[strings.TrimSpace(key)] = strings.Trim(
        strings.TrimSpace(value), `"'`,
      )
    }
  }

  return h, scanner.Err()
}

// oneLine flattens a description so a newline can't break the
// frontmatter or the index.
func oneLine(s string) string {
  return strings.Join(strings.Fields(s), " ")
}

// writeAtomic writes data via temp file + rename.
func writeAtomic(path string, data []byte) error {
  err := os.MkdirAll(filepath.Dir(path), 0o700)
  if err != nil {
    return err
  }

  tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
  if err != nil {
    return err
  }

  defer os.Remove(tmp.Name())

  _, err = tmp.Write(data)
  cerr := tmp.Close()
  if err == nil {
    err = cerr
  }

  if err != nil {
    return err
  }

  return os.Rename(tmp.Name(), path)
}

// instructionFiles are the checked-in agent instruction files looked
// for at the project root, first match wins.
var instructionFiles = []string{ "ARMIN.md", "AGENTS.md", "CLAUDE.md" }

// maxInstructionBytes caps how much of that file reaches the prompt.
const maxInstructionBytes = 16 * 1024

// ProjectInstructions returns the root's agent instruction file, if
// any: its name and (capped) content.
func ProjectInstructions(root string) (string, string) {
  for _, name := range instructionFiles {
    raw, err := os.ReadFile(filepath.Join(root, name))
    if err != nil {
      continue
    }

    if len(raw) > maxInstructionBytes {
      raw = append(raw[:maxInstructionBytes], "\n[... truncated]"...)
    }

    return name, string(raw)
  }

  return "", ""
}
