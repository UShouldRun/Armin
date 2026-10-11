package tools

import (
  "context"
  "fmt"
  "os"
  "path/filepath"
  "sort"
  "strings"

  "armin/internal/sandbox"
)

// ListDirectoryInput is the argument set for list_directory.
type ListDirectoryInput struct {
  Path  string `json:"path,omitempty"`  // default: project root
  Depth int    `json:"depth,omitempty"` // default: 2
}

type ListDirectoryOutput struct {
  Tree string `json:"tree"`
}

// skipDirs are noise directories we never descend into — keeps this
// tool cheap and the output signal-dense, which matters a lot with a
// small model's limited effective context.
var skipDirs = map[string]bool{
  ".git":         true,
  "node_modules": true,
  "vendor":       true,
  ".idea":        true,
  ".vscode":      true,
  "dist":         true,
  "build":        true,
}

// SkipDir reports whether a directory name is noise every walker
// skips (the web UI's file list uses the same set).
func SkipDir(name string) bool { return skipDirs[name] }

// ListDirectory returns an indented tree of the directory up to the
// requested depth.
func ListDirectory(
  ctx context.Context, cfg *sandbox.Config, in ListDirectoryInput,
) (*ListDirectoryOutput, error) {
  depth := in.Depth
  if depth <= 0 {
    depth = 2
  }

  resolved, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in ListDirectory:\n\t%w", err)
  }

  info, err := os.Stat(resolved)
  if err != nil {
    return nil, fmt.Errorf(
      "\n\t... in ListDirectory:\n\t stat %q: %w", in.Path, err,
    )
  }
  if !info.IsDir() {
    return nil, fmt.Errorf(
      "\n\t... in ListDirectory:\n\t %q is not a directory", in.Path,
    )
  }

  var b strings.Builder
  b.WriteString(".\n")
  if err := walk(&b, resolved, "", depth); err != nil {
    return nil, fmt.Errorf("\n\t... in ListDirectory:\n\t%w", err)
  }

  return &ListDirectoryOutput{ Tree: b.String() }, nil
}

func walk(b *strings.Builder, dir, prefix string, depthLeft int) error {
  if depthLeft <= 0 {
    return nil
  }

  entries, err := os.ReadDir(dir)
  if err != nil {
    return fmt.Errorf("\n\t... in walk:\n\t reading dir %q: %w", dir, err)
  }

  sort.Slice(entries, func(i, j int) bool {
    return entries[i].Name() < entries[j].Name()
  })

  kept := keptEntries(entries)

  for i, e := range kept {
    last := i == len(kept) - 1

    connector := "├── "
    nextPrefix := prefix + "│   "
    if last {
      connector = "└── "
      nextPrefix = prefix + "    "
    }

    name := e.Name()
    if e.IsDir() {
      name += "/"
    }
    b.WriteString(prefix + connector + name + "\n")

    if e.IsDir() {
      next := filepath.Join(dir, e.Name())
      if err := walk(b, next, nextPrefix, depthLeft - 1); err != nil {
        return err
      }
    }
  }

  return nil
}

func keptEntries(entries []os.DirEntry) []os.DirEntry {
  var kept []os.DirEntry

  for _, e := range entries {
    dotfile := strings.HasPrefix(e.Name(), ".") &&
      e.Name() != "." && !e.IsDir()
    if dotfile {
      continue // skip dotfiles, keep dotdirs filterable below
    }
    if e.IsDir() && skipDirs[e.Name()] {
      continue
    }
    kept = append(kept, e)
  }

  return kept
}
