// The tree's staleness check at session start: which files changed
// since the last session (git, or every cited file outside git), then
// know's CheckFile, MarkSuspect and FlagCycles on them.

package think

import (
  "context"
  "fmt"
  "os"
  "os/exec"
  "path/filepath"
  "strings"
  "time"

  "armin/internal/know"
)

// gitTimeout bounds each git command; a slow repository must not
// hold up the start.
const gitTimeout = 10 * time.Second

// refreshKnow brings the tree up to date with the code and says what
// it found, or "" when nothing changed.
func refreshKnow(ctx context.Context, root string, g *know.Graph) (string, error) {
  if g.ReadOnly() {
    return "", nil
  }

  cited := g.CitedFiles()
  byRel := map[string][]know.EvidenceID{}
  for path, evs := range cited {
    rel := path
    if filepath.IsAbs(path) {
      r, err := filepath.Rel(root, path)
      if err != nil || strings.HasPrefix(r, "..") {
        continue
      }

      rel = r
    }

    byRel[rel] = append(byRel[rel], evs...)
  }

  seenFile := filepath.Join(g.Dir(), "seen")
  head := git(ctx, root, "rev-parse", "HEAD")
  changed := changedFiles(ctx, root, seenFile, head, byRel)

  var stale []know.NodeID
  moved := 0
  for rel := range changed {
    evs := byRel[rel]
    if len(evs) == 0 {
      continue
    }

    data, err := os.ReadFile(filepath.Join(root, rel))
    gone := os.IsNotExist(err)
    if err != nil && !gone {
      continue
    }

    s, m, err := g.CheckFile(rel, data, gone, evs)
    if err != nil {
      return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
    }

    stale = append(stale, s...)
    moved += m
  }

  suspect, err := g.MarkSuspect(stale)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  author, err := g.Intern(know.RoleArmin, "", "", 0)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  loops, err := g.FlagCycles(author)
  if err != nil {
    return "", fmt.Errorf("\n\t... in refreshKnow:\n\t%w", err)
  }

  if head != "" {
    os.WriteFile(seenFile, []byte(head + "\n"), 0o600)
  }

  var parts []string
  for _, p := range []struct {
    n    int
    what string
  }{
    { len(stale), "stale" }, { suspect, "suspect" },
    { moved, "references followed moved code" }, { len(loops), "open requires loops" },
  } {
    if p.n > 0 {
      parts = append(parts, fmt.Sprintf("%d %s", p.n, p.what))
    }
  }

  if len(parts) == 0 {
    return "", nil
  }

  return "knowledge tree: " + strings.Join(parts, ", "), nil
}

// changedFiles is the set of cited files that may differ from what
// the tree last saw: committed since the last session's HEAD, changed
// in the working tree, or untracked. Without git, or without a known
// last HEAD, every cited file.
func changedFiles(
  ctx context.Context, root, seenFile, head string,
  cited map[string][]know.EvidenceID,
) map[string]bool {
  all := func() map[string]bool {
    out := map[string]bool{}
    for rel := range cited {
      out[rel] = true
    }

    return out
  }

  last, err := os.ReadFile(seenFile)
  seen := strings.TrimSpace(string(last))
  if head == "" || err != nil || seen == "" ||
    git(ctx, root, "cat-file", "-e", seen + "^{commit}") == "!" {
    return all()
  }

  out := map[string]bool{}
  for _, args := range [][]string{
    { "diff", "--name-only", "--relative", seen, "HEAD" },
    { "diff", "--name-only", "--relative", "HEAD" },
    { "ls-files", "--others", "--exclude-standard" },
  } {
    for _, f := range strings.Split(git(ctx, root, args...), "\n") {
      f = strings.TrimSpace(f)
      if f != "" && f != "!" {
        out[filepath.Clean(f)] = true
      }
    }
  }

  return out
}

// git runs a git command in root and returns its trimmed output, ""
// when there's none, or "!" when it failed.
func git(ctx context.Context, root string, args ...string) string {
  ctx, cancel := context.WithTimeout(ctx, gitTimeout)
  defer cancel()

  cmd := exec.CommandContext(ctx, "git", append([]string{ "-C", root }, args...)...)
  out, err := cmd.Output()
  if err != nil {
    if len(args) > 0 && args[0] == "rev-parse" {
      return ""
    }

    return "!"
  }

  return strings.TrimSpace(string(out))
}
