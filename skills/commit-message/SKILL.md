---
name: commit-message
description: Write commits the user's way — "vX.Y.Z - what changed" subjects with semver bumps, short lowercase summaries, no AI attribution. Load before committing.
---
## Format
```
v0.3.4 - chess.com-style pieces and board, title-screen menu
```
- Subject: the new version, ` - `, then a short lowercase summary of
  what changed, comma-separated if several things. No trailing period.
- Bump from the last version in `git log`:
  - patch (`v1.3.0 → v1.3.1`): fixes, small corrections
  - minor (`v0.2.x → v0.3.0`): new features or behavior
  - major: breaking API or format changes (only when the user says)
- If the repo doesn't use versioned subjects, follow what `git log`
  shows instead.
- A body only when the why isn't obvious: wrap at 72, plain sentences.
- **No AI attribution**: no `Co-Authored-By: Claude`, no "Generated
  with" lines.

## Steps
1. `git status` and `git diff` (staged and not): know exactly what
   goes in. Don't commit build output, secrets (.env, keys, tokens)
   or unrelated changes — ask about anything unexpected.
2. `git log --oneline -10` for the current version and wording.
3. Stage specific paths, not `git add .`, unless everything shown is
   meant to go in.
4. Commit only when the user asked to; push only when asked.
