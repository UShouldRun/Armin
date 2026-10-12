package tools

import (
  "bufio"
  "context"
  "fmt"
  "sort"
  "strconv"
  "strings"
  "time"

  "armin/internal/sandbox"
)

// gitLogSeparator is a unit separator (0x1f), not a printable
// character, so it can't collide with anything a real commit
// message/author/date would contain.
const gitLogSeparator = "\x1f"

func clampCount(n, def, max int) int {
  switch {
  case n <= 0:
    return def
  case n > max:
    return max
  default:
    return n
  }
}

func firstLine(s string) string {
  i := strings.IndexByte(s, '\n')
  if i >= 0 {
    return s[:i]
  }
  return s
}

// --- git_log ---

const (
  defaultLogCount = 20
  maxLogCount     = 100
)

// GitLog returns structured commit history: a fixed --format string
// parsed into fields, not raw git log text the model has to
// re-parse itself.
func GitLog(
  ctx context.Context, cfg *sandbox.Config, in GitLogInput,
) (*GitLogOutput, error) {
  count := clampCount(in.Count, defaultLogCount, maxLogCount)

  args := []string{
    "log", "-n", strconv.Itoa(count),
    "--format=%h" + gitLogSeparator + "%an" + gitLogSeparator +
      "%ad" + gitLogSeparator + "%s",
    "--date=short",
  }
  if in.Path != "" {
    resolved, err := cfg.ResolvePath(in.Path)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in GitLog:\n\t%w", err)
    }
    args = append(args, "--", resolved)
  }

  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in GitLog:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in GitLog:\n\t git log failed: %s", firstLine(res.Stderr),
    )
  }

  out := &GitLogOutput{}
  for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
    if line == "" {
      continue
    }
    parts := strings.SplitN(line, gitLogSeparator, 4)
    if len(parts) != 4 {
      continue
    }
    out.Commits = append(out.Commits, GitCommit{
      Hash: parts[0], Author: parts[1], Date: parts[2], Subject: parts[3],
    })
  }
  return out, nil
}

// --- git_blame ---

const maxBlameLines = 200

// GitBlame parses `git blame --line-porcelain`'s output into
// structured per-line attribution, instead of the raw porcelain
// format the model would otherwise have to make sense of.
func GitBlame(
  ctx context.Context, cfg *sandbox.Config, in GitBlameInput,
) (*GitBlameOutput, error) {
  if in.Path == "" {
    return nil, fmt.Errorf("\n\t... in GitBlame:\n\t path is required")
  }
  resolved, err := cfg.ResolvePath(in.Path)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in GitBlame:\n\t%w", err)
  }

  start := in.StartLine
  if start < 1 {
    start = 1
  }
  end := in.EndLine
  if end == 0 || end < start || end - start + 1 > maxBlameLines {
    end = start + maxBlameLines - 1
  }

  args := []string{
    "blame", "--line-porcelain",
    "-L", fmt.Sprintf("%d,%d", start, end), resolved,
  }
  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in GitBlame:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in GitBlame:\n\t git blame failed: %s", firstLine(res.Stderr),
    )
  }

  return &GitBlameOutput{ Lines: parseBlame(res.Stdout, start) }, nil
}

// parseBlame reads --line-porcelain's per-line blocks: one commit
// header line, several "key value" metadata lines, then a line
// starting with a tab holding the actual source line. startLine
// numbers the output sequentially since -L's requested range is
// already known, rather than re-deriving it from porcelain's own
// markers.
func parseBlame(porcelain string, startLine int) []BlameLine {
  var out []BlameLine
  var cur BlameLine
  lineNo := startLine

  scanner := bufio.NewScanner(strings.NewReader(porcelain))
  scanner.Buffer(make([]byte, 0, 64 * 1024), 1024 * 1024)
  for scanner.Scan() {
    line := scanner.Text()
    switch {
    case strings.HasPrefix(line, "\t"):
      cur.Line = lineNo
      out = append(out, cur)
      cur = BlameLine{}
      lineNo++
    case strings.HasPrefix(line, "author "):
      cur.Author = strings.TrimPrefix(line, "author ")
    case strings.HasPrefix(line, "author-time "):
      ts, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
      if err == nil {
        cur.Date = time.Unix(ts, 0).UTC().Format("2006-01-02")
      }
    case strings.HasPrefix(line, "summary "):
      cur.Summary = strings.TrimPrefix(line, "summary ")
    }
  }
  return out
}

// --- git_diff ---

// GitDiff diffs From..To (default HEAD vs. the working tree) for
// Path (default the whole repo).
func GitDiff(
  ctx context.Context, cfg *sandbox.Config, in GitDiffInput,
) (*GitDiffOutput, error) {
  from := in.From
  if from == "" {
    from = "HEAD"
  }

  args := []string{ "diff" }
  if in.To != "" {
    args = append(args, from + ".." + in.To)
  } else {
    args = append(args, from)
  }
  if in.Path != "" {
    resolved, err := cfg.ResolvePath(in.Path)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in GitDiff:\n\t%w", err)
    }
    args = append(args, "--", resolved)
  }

  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in GitDiff:\n\t%w", err)
  }
  if res.ExitCode > 1 {
    return nil, fmt.Errorf(
      "\n\t... in GitDiff:\n\t git diff failed: %s", firstLine(res.Stderr),
    )
  }

  diff := res.Stdout
  if diff == "" {
    diff = "(no differences)"
  }
  return &GitDiffOutput{ Diff: diff, Truncated: res.Flags.Truncated() }, nil
}

// --- git_show ---

// GitShow returns one commit's full diff and message by hash.
func GitShow(
  ctx context.Context, cfg *sandbox.Config, in GitShowInput,
) (*GitShowOutput, error) {
  if in.Hash == "" {
    return nil, fmt.Errorf("\n\t... in GitShow:\n\t hash is required")
  }

  res, err := cfg.RunForce(ctx, "git", []string{ "show", in.Hash })
  if err != nil {
    return nil, fmt.Errorf("\n\t... in GitShow:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in GitShow:\n\t git show failed: %s", firstLine(res.Stderr),
    )
  }
  return &GitShowOutput{
    Show: res.Stdout, Truncated: res.Flags.Truncated(),
  }, nil
}

// --- working_tree_diff ---

// WorkingTreeDiff is uncommitted changes only — distinct from
// git_diff so "what did I just change" doesn't require the model to
// already know git's ref syntax.
func WorkingTreeDiff(
  ctx context.Context, cfg *sandbox.Config, in WorkingTreeDiffInput,
) (*WorkingTreeDiffOutput, error) {
  args := []string{ "diff" }
  if in.Path != "" {
    resolved, err := cfg.ResolvePath(in.Path)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in WorkingTreeDiff:\n\t%w", err)
    }
    args = append(args, "--", resolved)
  }

  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in WorkingTreeDiff:\n\t%w", err)
  }
  if res.ExitCode > 1 {
    return nil, fmt.Errorf(
      "\n\t... in WorkingTreeDiff:\n\t git diff failed: %s", firstLine(res.Stderr),
    )
  }

  out := &WorkingTreeDiffOutput{
    Diff: res.Stdout, Truncated: res.Flags.Truncated(),
  }
  if out.Diff == "" {
    out.Diff = "(no uncommitted changes)"
  }

  statusArgs := []string{ "status", "--porcelain", "--untracked-files=all" }
  statusRes, err := cfg.RunForce(ctx, "git", statusArgs)
  if err == nil {
    for _, line := range strings.Split(strings.TrimRight(statusRes.Stdout, "\n"), "\n") {
      if strings.HasPrefix(line, "?? ") {
        out.Untracked = append(out.Untracked, strings.TrimPrefix(line, "?? "))
      }
    }
  }
  return out, nil
}

// --- grep_history ---

const (
  defaultGrepHistoryCount = 10
  maxGrepHistoryCount     = 50
)

// GrepHistory finds when Term was introduced or removed — git log's
// -S (pickaxe) search, scoped to Path if given.
func GrepHistory(
  ctx context.Context, cfg *sandbox.Config, in GrepHistoryInput,
) (*GrepHistoryOutput, error) {
  if in.Term == "" {
    return nil, fmt.Errorf("\n\t... in GrepHistory:\n\t term is required")
  }
  count := clampCount(in.Count, defaultGrepHistoryCount, maxGrepHistoryCount)

  args := []string{
    "log", "-n", strconv.Itoa(count), "-S", in.Term, "--oneline", "--patch",
  }
  if in.Path != "" {
    resolved, err := cfg.ResolvePath(in.Path)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in GrepHistory:\n\t%w", err)
    }
    args = append(args, "--", resolved)
  }

  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in GrepHistory:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in GrepHistory:\n\t git log failed: %s", firstLine(res.Stderr),
    )
  }

  log := res.Stdout
  if log == "" {
    log = "(no matching commits)"
  }
  return &GrepHistoryOutput{ Log: log, Truncated: res.Flags.Truncated() }, nil
}

// --- code_owners ---

// CodeOwners approximates "who owns this" from commit-count-per-
// author on Path — a heuristic, not a real CODEOWNERS lookup: it
// says who commits here most, not who's authoritative.
func CodeOwners(
  ctx context.Context, cfg *sandbox.Config, in CodeOwnersInput,
) (*CodeOwnersOutput, error) {
  args := []string{ "log", "--format=%an" }
  if in.Path != "" {
    resolved, err := cfg.ResolvePath(in.Path)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in CodeOwners:\n\t%w", err)
    }
    args = append(args, "--", resolved)
  }

  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in CodeOwners:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in CodeOwners:\n\t git log failed: %s", firstLine(res.Stderr),
    )
  }

  counts := map[string]int{}
  for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
    if line != "" {
      counts[line]++
    }
  }

  out := &CodeOwnersOutput{}
  for author, n := range counts {
    out.Owners = append(out.Owners, AuthorCount{ Author: author, Commits: n })
  }
  sort.Slice(out.Owners, func(i, j int) bool {
    if out.Owners[i].Commits != out.Owners[j].Commits {
      return out.Owners[i].Commits > out.Owners[j].Commits
    }
    return out.Owners[i].Author < out.Owners[j].Author
  })
  return out, nil
}

// --- commit_stats ---

const (
  defaultHotspotCount = 10
  maxHotspotCount     = 50
)

// CommitStats ranks files by how often they've changed — a churn/
// hotspot signal: frequently-touched code is worth extra scrutiny,
// not necessarily a red flag on its own.
func CommitStats(
  ctx context.Context, cfg *sandbox.Config, in CommitStatsInput,
) (*CommitStatsOutput, error) {
  n := clampCount(in.Count, defaultHotspotCount, maxHotspotCount)

  args := []string{ "log", "--format=format:", "--name-only" }
  if in.Path != "" {
    resolved, err := cfg.ResolvePath(in.Path)
    if err != nil {
      return nil, fmt.Errorf("\n\t... in CommitStats:\n\t%w", err)
    }
    args = append(args, "--", resolved)
  }

  res, err := cfg.RunForce(ctx, "git", args)
  if err != nil {
    return nil, fmt.Errorf("\n\t... in CommitStats:\n\t%w", err)
  }
  if res.ExitCode != 0 {
    return nil, fmt.Errorf(
      "\n\t... in CommitStats:\n\t git log failed: %s", firstLine(res.Stderr),
    )
  }

  counts := map[string]int{}
  for _, line := range strings.Split(res.Stdout, "\n") {
    if line != "" {
      counts[line]++
    }
  }

  files := make([]FileChangeCount, 0, len(counts))
  for f, c := range counts {
    files = append(files, FileChangeCount{ File: f, Commits: c })
  }
  sort.Slice(files, func(i, j int) bool {
    if files[i].Commits != files[j].Commits {
      return files[i].Commits > files[j].Commits
    }
    return files[i].File < files[j].File
  })
  if len(files) > n {
    files = files[:n]
  }
  return &CommitStatsOutput{ Hotspots: files }, nil
}
