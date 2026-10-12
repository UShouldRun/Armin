package tools

import (
  "context"
  "os/exec"
  "strings"
  "testing"

  "armin/internal/sandbox"
)

func requireGit(t *testing.T) {
  t.Helper()
  _, err := exec.LookPath("git")
  if err != nil {
    t.Skip("git not available — skipping git-backed tool tests")
  }
}

// initGitRepo makes cfg.Root a real git repo with one commit
// touching a.txt, so git_log/git_blame/code_owners/commit_stats
// have real history to read.
func initGitRepo(t *testing.T, cfg *sandbox.Config) {
  t.Helper()
  run := func(args ...string) {
    res, err := cfg.RunForce(context.Background(), "git", args)
    if err != nil {
      t.Fatalf("git %v: %v", args, err)
    }
    if res.ExitCode != 0 {
      t.Fatalf("git %v exited %d: %s", args, res.ExitCode, res.Stderr)
    }
  }

  writeFile(t, cfg.Root, "a.txt", "hello\n")
  run("init", "-q")
  run("config", "user.email", "test@example.com")
  run("config", "user.name", "Test User")
  run("add", "a.txt")
  run("commit", "-q", "-m", "initial commit")
}

// --- git_log ---

func TestGitLogParsesCommits(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := GitLog(context.Background(), cfg, GitLogInput{})
  if err != nil {
    t.Fatalf("GitLog: %v", err)
  }
  if len(out.Commits) != 1 {
    t.Fatalf("Commits = %+v, want 1", out.Commits)
  }
  c := out.Commits[0]
  if c.Author != "Test User" || c.Subject != "initial commit" || c.Hash == "" || c.Date == "" {
    t.Fatalf("commit = %+v, missing/wrong fields", c)
  }
}

func TestGitLogNotARepo(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  writeFile(t, cfg.Root, "a.txt", "x")

  _, err := GitLog(context.Background(), cfg, GitLogInput{})
  if err == nil {
    t.Fatal("expected an error when the root isn't a git repo")
  }
}

// --- git_blame ---

func TestGitBlameAttributesLines(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := GitBlame(context.Background(), cfg, GitBlameInput{Path: "a.txt"})
  if err != nil {
    t.Fatalf("GitBlame: %v", err)
  }
  if len(out.Lines) != 1 {
    t.Fatalf("Lines = %+v, want 1", out.Lines)
  }
  if out.Lines[0].Author != "Test User" || out.Lines[0].Line != 1 {
    t.Fatalf("Lines[0] = %+v", out.Lines[0])
  }
}

func TestGitBlameRequiresPath(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := GitBlame(context.Background(), cfg, GitBlameInput{})
  if err == nil {
    t.Fatal("expected an error for an empty path")
  }
}

// --- git_diff / working_tree_diff ---

func TestWorkingTreeDiffShowsUncommittedAndUntracked(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  writeFile(t, cfg.Root, "a.txt", "hello\nworld\n")
  writeFile(t, cfg.Root, "new.txt", "new file")

  out, err := WorkingTreeDiff(context.Background(), cfg, WorkingTreeDiffInput{})
  if err != nil {
    t.Fatalf("WorkingTreeDiff: %v", err)
  }
  if !strings.Contains(out.Diff, "world") {
    t.Fatalf("Diff = %q, want it to show the uncommitted change", out.Diff)
  }
  if len(out.Untracked) != 1 || out.Untracked[0] != "new.txt" {
    t.Fatalf("Untracked = %v, want [new.txt]", out.Untracked)
  }
}

func TestWorkingTreeDiffCleanTree(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := WorkingTreeDiff(context.Background(), cfg, WorkingTreeDiffInput{})
  if err != nil {
    t.Fatalf("WorkingTreeDiff: %v", err)
  }
  if out.Diff != "(no uncommitted changes)" {
    t.Fatalf("Diff = %q, want the clean-tree sentinel", out.Diff)
  }
}

func TestGitDiffAgainstHead(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)
  writeFile(t, cfg.Root, "a.txt", "hello\nchanged\n")

  out, err := GitDiff(context.Background(), cfg, GitDiffInput{})
  if err != nil {
    t.Fatalf("GitDiff: %v", err)
  }
  if !strings.Contains(out.Diff, "changed") {
    t.Fatalf("Diff = %q, want it to show the change", out.Diff)
  }
}

// --- git_show ---

func TestGitShowRequiresHash(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := GitShow(context.Background(), cfg, GitShowInput{})
  if err == nil {
    t.Fatal("expected an error for an empty hash")
  }
}

func TestGitShowReturnsCommit(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := GitShow(context.Background(), cfg, GitShowInput{Hash: "HEAD"})
  if err != nil {
    t.Fatalf("GitShow: %v", err)
  }
  if !strings.Contains(out.Show, "initial commit") {
    t.Fatalf("Show = %q, want it to contain the commit message", out.Show)
  }
}

// --- grep_history ---

func TestGrepHistoryRequiresTerm(t *testing.T) {
  cfg := newTestSandbox(t, sandbox.Options{})
  _, err := GrepHistory(context.Background(), cfg, GrepHistoryInput{})
  if err == nil {
    t.Fatal("expected an error for an empty term")
  }
}

func TestGrepHistoryFindsIntroducedText(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := GrepHistory(context.Background(), cfg, GrepHistoryInput{Term: "hello"})
  if err != nil {
    t.Fatalf("GrepHistory: %v", err)
  }
  if out.Log == "(no matching commits)" {
    t.Fatal("expected to find the commit that introduced \"hello\"")
  }
}

// --- code_owners / commit_stats ---

func TestCodeOwnersRanksByCommitCount(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := CodeOwners(context.Background(), cfg, CodeOwnersInput{})
  if err != nil {
    t.Fatalf("CodeOwners: %v", err)
  }
  if len(out.Owners) != 1 || out.Owners[0].Author != "Test User" || out.Owners[0].Commits != 1 {
    t.Fatalf("Owners = %+v", out.Owners)
  }
}

func TestCommitStatsRanksHotspots(t *testing.T) {
  requireGit(t)
  cfg := newTestSandbox(t, sandbox.Options{})
  initGitRepo(t, cfg)

  out, err := CommitStats(context.Background(), cfg, CommitStatsInput{})
  if err != nil {
    t.Fatalf("CommitStats: %v", err)
  }
  if len(out.Hotspots) != 1 || out.Hotspots[0].File != "a.txt" || out.Hotspots[0].Commits != 1 {
    t.Fatalf("Hotspots = %+v", out.Hotspots)
  }
}
