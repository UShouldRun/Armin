---
name: git
description: Work with git safely — inspect before changing, branch and commit the user's way, resolve conflicts, rebase/merge, recover with reflog, and never rewrite shared history or discard work without asking. Load for any git task beyond reading history.
---
## Look before you change anything
- `git status`, `git diff` (unstaged), `git diff --staged`,
  `git log --oneline -15 --graph`. Know the branch, what's staged and
  what's untracked before running anything that changes state.
- armin's read tools cover most inspection: `git_log`, `git_diff`,
  `git_show`, `git_blame`, `working_tree_diff`, `grep_history`.

## Committing
- Follow the commit-message skill (`vX.Y.Z - summary`, no AI
  attribution). Commit only when asked; push only when asked.
- Stage explicit paths (`git add path/a path/b`), not `git add .`,
  unless everything in `git status` is meant to go in. Never commit
  secrets (.env, keys, tokens) or build output.
- One logical change per commit. Review `git diff --staged` first.

## Branches
- Work on a branch for anything non-trivial:
  `git switch -c feature/short-name`. Don't commit to main directly
  unless the user works that way.
- Update a branch from main with `git fetch` then
  `git rebase origin/main` on an unshared branch, or
  `git merge origin/main` on a shared one.

## Conflicts
1. `git status` lists the conflicted files.
2. Read each conflict (`<<<<<<<` … `>>>>>>>`), understand both sides,
   and keep the intended result; don't just pick one side blindly.
3. Build and test, then `git add` the files and
   `git rebase --continue` / `git commit`.
4. `git rebase --abort` / `git merge --abort` gets back to the start.

## Undoing (least destructive first)
- Unstage: `git restore --staged <file>`.
- Amend the last *unpushed* commit: `git commit --amend`.
- Undo a pushed commit: `git revert <sha>` (a new commit), not reset.
- Find "lost" commits: `git reflog`, then `git switch -c rescue <sha>`.
- Stash work in progress: `git stash push -m "why"`, `git stash list`,
  `git stash pop`.

## Never without the user's explicit OK
`git push --force` (use `--force-with-lease` if they agree), `git
reset --hard`, `git clean -fd`, `git checkout -- .`, `git branch -D`,
rebasing a branch others use, rewriting tags. These destroy work or
rewrite history others have. Say what would be lost and ask.

## Useful
- Who changed this and why: `git log -L 10,30:file.go`,
  `git log -S'symbol' --oneline`.
- What a branch adds: `git log main..HEAD --oneline`,
  `git diff main...HEAD`.
- Bisect a regression: `git bisect start`, `git bisect bad`,
  `git bisect good <sha>`, test, repeat; `git bisect reset`.
