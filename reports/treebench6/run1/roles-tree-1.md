How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 66943 prompt (49280 cached), 1014 completion, $0.00296, 5 rounds, 6 tools

## answer


It's a bitmask per tool, checked in two places:

**The masks** (`roles` field on each tool spec in `internal/agent/*.go`):
- No `roles` set → available to **all** roles (search, read, know, git, web, ...)
- `edit_file`, `write_file` → `RoleMinion | RoleCopilot` (tools_files.go:100,136)
- `run_program` → `RoleCopilot` only (tools_run.go:50)
- `delegate` → `RoleCopilot | RoleAdvisor` (tools_delegate.go:42)
- `learn_explained` → `RoleAdvisor` only (tools_teach.go:25)

So: **minion** = everything except `run_program` and `delegate`; **copilot** = everything except `learn_explained`; **advisor** = reads/learning, no `edit_file`/`write_file`/`run_program`.

**Two enforcement layers:**
1. *Offering* — `Loop.offered()` (tools.go:116-119) filters the tool list via `id.allows(l.Role)` (role.go:83-86: `r == 0 || roles == 0 || roles & r != 0`), so the model never sees a disallowed tool in its definitions.
2. *Execution* — `Loop.execute` re-checks (tools.go:193-199); if the model calls it anyway, it gets an error back: "the advisor role can't use write_file; the user can switch roles with /copilot".

Both are pinned by tests: `role_test.go:24-28` (advisor not offered the write tools) and `role_test.go:55-65` (runtime refusal).

Side effects beyond the tool list: a role switch just prepends a reminder to the next user message (role.go:79-115, `roleReminders`) instead of rewriting the system prompt, to preserve the prompt cache. Minion's knowledge-tree writes are capped at `inferred` confidence and marked as the minion's (tools_know.go:532 `minionLimits`), and it can't use `delegate` (tools_delegate.go) — only the copilot/advisor delegate *to* the minion.

## calls
search_code map[pattern:minion]
search_code map[pattern:copilot]
search_code map[file_type:go path:internal/agent pattern:RoleMinion|RoleCopilot|RoleAdvisor]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:\s*Role]
search_code map[file_type:go path:internal/agent pattern:allows\(|can't use]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793473424974848350"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "tools_files.go:100 tools_run.go:50 tools_delegate.go:42 tools_teach.go:25 tools.go:116-119 role.go:83-86 tools.go:193-199 role_test.go:24-28 role_test.go:55-65 role.go:79-115 tools_know.go:532"
  evidence session "internal/agent/role.go" 00000000

