package tools

import (
  "fmt"
  "os"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/sandbox"
)

func NewFileTracker() *FileTracker {
  return &FileTracker{ seen: make(map[string]time.Time) }
}

// Mark records resolved as read at its current mtime.
func (t *FileTracker) Mark(resolved string) {
  if t == nil {
    return
  }

  info, err := os.Stat(resolved)
  if err != nil {
    return
  }

  t.mu.Lock()
  t.seen[resolved] = info.ModTime()
  t.mu.Unlock()
}

// check fails when resolved exists but wasn't read, or changed since.
// A nil tracker (MCP: the client owns that discipline) never fails.
func (t *FileTracker) check(resolved, path string) error {
  if t == nil {
    return nil
  }

  info, err := os.Stat(resolved)
  if os.IsNotExist(err) {
    return nil
  }

  if err != nil {
    return err
  }

  t.mu.Lock()
  at, ok := t.seen[resolved]
  t.mu.Unlock()

  if !ok {
    return fmt.Errorf(
      "%q exists and hasn't been read this session — read_file "+
        "it first so the change is based on its real content",
      path,
    )
  }

  if !info.ModTime().Equal(at) {
    return fmt.Errorf(
      "%q changed on disk since you last read it — read_file "+
        "it again before changing it",
      path,
    )
  }

  return nil
}

// resolveWritable jails path and refuses anything inside .git.
func resolveWritable(
  cfg *sandbox.Config, path string,
) (string, error) {
  if strings.TrimSpace(path) == "" {
    return "", fmt.Errorf("path is required")
  }

  resolved, err := cfg.ResolvePath(path)
  if err != nil {
    return "", err
  }

  // /add-dir grants reading only: writes stay inside the project.
  if !cfg.InRoot(resolved) {
    return "", fmt.Errorf("%q is outside the project: read-only", path)
  }

  rel, err := filepath.Rel(cfg.Root, resolved)
  if err != nil {
    return "", err
  }

  first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
  if first == ".git" {
    return "", fmt.Errorf("refusing to write inside .git: %q", path)
  }

  if rel == "." {
    return "", fmt.Errorf("%q is the project root, not a file", path)
  }

  return resolved, nil
}

// --- write_file ---

// PlanWrite prepares creating or overwriting a whole file.
func PlanWrite(
  cfg *sandbox.Config, tracker *FileTracker, in WriteFileInput,
) (*Change, error) {
  resolved, err := resolveWritable(cfg, in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanWrite:\n\t%w", err)
  }

  c := &Change{
    Path:     in.Path,
    Resolved: resolved,
    New:      in.Content,
    mode:     0o644,
  }

  info, err := os.Stat(resolved)
  switch {
  case os.IsNotExist(err):
    c.Created = true

    return c, nil
  case err != nil:
    return nil, fmt.Errorf("\n\t... in PlanWrite:\n\t%w", err)
  case info.IsDir():
    return nil, fmt.Errorf(
      "\n\t... in PlanWrite:\n\t %q is a directory", in.Path,
    )
  }

  err = tracker.check(resolved, in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanWrite:\n\t%w", err)
  }

  old, err := os.ReadFile(resolved)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanWrite:\n\t%w", err)
  }

  c.Old, c.mode = string(old), info.Mode().Perm()

  return c, nil
}

// --- edit_file ---

// PlanEdit prepares an exact-string replacement. old_string must
// match exactly once unless replace_all is set — an ambiguous match
// is an error, never a guess.
func PlanEdit(
  cfg *sandbox.Config, tracker *FileTracker, in EditFileInput,
) (*Change, error) {
  resolved, err := resolveWritable(cfg, in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanEdit:\n\t%w", err)
  }

  if in.OldString == "" {
    return nil, fmt.Errorf(
      "\n\t... in PlanEdit:\n\t old_string is empty — use " +
        "write_file to create a file",
    )
  }

  if in.OldString == in.NewString {
    return nil, fmt.Errorf(
      "\n\t... in PlanEdit:\n\t old_string and new_string are identical",
    )
  }

  info, err := os.Stat(resolved)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanEdit:\n\t%w", err)
  }

  err = tracker.check(resolved, in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanEdit:\n\t%w", err)
  }

  raw, err := os.ReadFile(resolved)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in PlanEdit:\n\t%w", err)
  }

  old := string(raw)

  n := strings.Count(old, in.OldString)
  switch {
  case n == 0:
    return nil, fmt.Errorf(
      "\n\t... in PlanEdit:\n\t old_string not found in %q%s",
      in.Path, nearMiss(old, in.OldString),
    )
  case n > 1 && !in.ReplaceAll:
    return nil, fmt.Errorf(
      "\n\t... in PlanEdit:\n\t old_string matches %d times in %q "+
        "— add surrounding lines to make it unique, or set "+
        "replace_all",
      n, in.Path,
    )
  }

  var updated string
  if in.ReplaceAll {
    updated = strings.ReplaceAll(old, in.OldString, in.NewString)
  } else {
    updated = strings.Replace(old, in.OldString, in.NewString, 1)
  }

  return &Change{
    Path:         in.Path,
    Resolved:     resolved,
    Old:          old,
    New:          updated,
    Replacements: n,
    mode:         info.Mode().Perm(),
  }, nil
}

// nearMiss points at where old_string's first line appears with
// different whitespace — the usual reason a small model's
// old_string doesn't match.
func nearMiss(content, oldString string) string {
  first, _, _ := strings.Cut(oldString, "\n")
  first = strings.TrimSpace(first)
  if first == "" {
    return ""
  }

  var hits []string
  for i, line := range strings.Split(content, "\n") {
    if strings.TrimSpace(line) == first {
      hits = append(hits, fmt.Sprint(i + 1))
      if len(hits) == 5 {
        break
      }
    }
  }

  if len(hits) == 0 {
    return " — read_file the region again and copy it exactly"
  }

  return fmt.Sprintf(
    " — its first line appears (ignoring indentation) at line %s; "+
      "re-read there and copy whitespace exactly",
    strings.Join(hits, ", "),
  )
}

// Apply writes the change atomically: a temp file in the same
// directory, then rename, so a crash never leaves half a file. With
// cfg.DryRun nothing is written.
func (c *Change) Apply(
  cfg *sandbox.Config, tracker *FileTracker,
) (*ChangeOutput, error) {
  added, removed := diffCounts(c.Old, c.New)
  out := &ChangeOutput{
    Path:         c.Path,
    Created:      c.Created,
    Replacements: c.Replacements,
    LinesAdded:   added,
    LinesRemoved: removed,
  }

  if cfg.DryRun {
    out.DryRun = true
    return out, nil
  }

  dir := filepath.Dir(c.Resolved)
  err := os.MkdirAll(dir, 0o755)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Apply:\n\t%w", err)
  }

  tmp, err := os.CreateTemp(dir, ".armin-*")
  if err != nil {
    return nil, fmt.Errorf("\n\t... in Apply:\n\t%w", err)
  }

  defer os.Remove(tmp.Name()) // no-op once renamed

  _, err = tmp.WriteString(c.New)
  if err == nil {
    err = tmp.Chmod(c.mode)
  }

  cerr := tmp.Close()
  if err == nil {
    err = cerr
  }

  if err == nil {
    err = os.Rename(tmp.Name(), c.Resolved)
  }

  if err != nil {
    return nil, fmt.Errorf("\n\t... in Apply:\n\t%w", err)
  }

  tracker.Mark(c.Resolved)

  return out, nil
}

// WriteFile plans and applies in one step, for callers with no
// approval step of their own (MCP: the client asks the user).
func WriteFile(
  cfg *sandbox.Config, tracker *FileTracker, in WriteFileInput,
) (*ChangeOutput, error) {
  c, err := PlanWrite(cfg, tracker, in)
  if err != nil {
    return nil, err
  }

  return c.Apply(cfg, tracker)
}

// EditFile is WriteFile's counterpart for edit_file.
func EditFile(
  cfg *sandbox.Config, tracker *FileTracker, in EditFileInput,
) (*ChangeOutput, error) {
  c, err := PlanEdit(cfg, tracker, in)
  if err != nil {
    return nil, err
  }

  return c.Apply(cfg, tracker)
}

// --- diff preview ---

// Hunk is the changed region: where it starts (0-based line), the
// lines it removes and the ones it adds, plus the old and new file
// as lines for context around it.
func (c *Change) Hunk() (start int, gone, came, oldLines, newLines []string) {
  oldLines, newLines = splitLines(c.Old), splitLines(c.New)
  start, gone, came = trimCommon(oldLines, newLines)

  return start, gone, came, oldLines, newLines
}

// maxPreviewLines caps the preview shown before approval.
const maxPreviewLines = 80

// trimCommon strips the lines a and b share at both ends. What's
// left is the changed region: O(n), no LCS table — enough for a
// preview and exact for the single-region edits edit_file makes.
func trimCommon(a, b []string) (start int, aMid, bMid []string) {
  for start < len(a) && start < len(b) && a[start] == b[start] {
    start++
  }

  ea, eb := len(a), len(b)
  for ea > start && eb > start && a[ea - 1] == b[eb - 1] {
    ea--
    eb--
  }

  return start, a[start:ea], b[start:eb]
}

func splitLines(s string) []string {
  if s == "" {
    return nil
  }

  return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

func diffCounts(old, new string) (added, removed int) {
  _, a, b := trimCommon(splitLines(old), splitLines(new))
  return len(b), len(a)
}

// Preview renders the change as "-"/"+" lines with a little context,
// unstyled — the caller colors it.
func (c *Change) Preview() string {
  oldLines, newLines := splitLines(c.Old), splitLines(c.New)
  start, gone, came := trimCommon(oldLines, newLines)

  var b strings.Builder
  if c.Created {
    fmt.Fprintf(&b, "new file %s (%d lines)\n", c.Path, len(newLines))
  } else {
    fmt.Fprintf(&b, "%s @ line %d\n", c.Path, start + 1)
  }

  const context = 2
  from := max(0, start - context)
  shown := 0
  emit := func(prefix, line string) bool {
    if shown == maxPreviewLines {
      return false
    }

    b.WriteString(prefix)
    b.WriteString(line)
    b.WriteByte('\n')
    shown++

    return true
  }

  for _, l := range oldLines[from:start] {
    emit("  ", l)
  }

  for _, l := range gone {
    if !emit("- ", l) {
      break
    }
  }

  for _, l := range came {
    if !emit("+ ", l) {
      break
    }
  }

  after := start + len(gone)
  for _, l := range oldLines[after:min(len(oldLines), after + context)] {
    emit("  ", l)
  }

  total := len(gone) + len(came)
  if shown == maxPreviewLines {
    fmt.Fprintf(&b, "… (%d changed lines, preview capped)\n", total)
  }

  return b.String()
}
