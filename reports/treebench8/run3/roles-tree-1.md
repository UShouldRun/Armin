How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 58843 prompt (49168 cached), 1144 completion, $0.00282, 5 rounds, 7 tools

## answer


I have what I need — the answer is clear from the code.

Roles limit tools in two layers, with the restriction declared per-tool:

**1. Declaration: each tool spec names its allowed roles** (`internal/agent/role.type.go:15-19` defines the bitmask — `RoleMinion`, `RoleCopilot`, `RoleAdvisor`):

- `edit_file`, `write_file` → `RoleMinion | RoleCopilot` (`tools_files.go:100`, `:136`)
- `run_program` → `RoleCopilot` only (`tools_run.go:50`)
- `learn_explained` → `RoleAdvisor` only (`tools_teach.go:25`)
- `delegate` → `RoleCopilot | RoleAdvisor` (`tools_delegate.go:43`)
- A spec with `roles: 0` admits every role.

So in effect: the **minion** has everything except `run_program`; the **copilot** has everything; the **advisor** has no file changes (`edit_file`, `write_file`, `run_program` are all off for it). That matches the design: minion = searches and small edits, copilot = full agent, advisor = read-only decisions/teaching.

**2. Enforcement in two places** (`internal/agent/tools.go`):

- **Offerting** — `Loop.offered` (`tools.go:116-118`) returns false when `!id.allows(l.Role)`, so `Loop.Definitions` (`tools.go:140-160`) builds a per-role tool list (cached once per role in `roleDefs`) that never even lists a disallowed tool to the model. `allows` (`role.go:83-86`) is just `r == 0 || roles == 0 || roles & r != 0` — no role set means all tools.
- **Runtime** — if the model calls a disallowed tool anyway, `Loop.execute` rejects it (`tools.go:195-199`) with an error like `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

Two related details:

- Switching roles (`/minion`, `/copilot`, `/advisor` → `SetRole`, `role.go:92-99`) doesn't rewrite the system prompt; it queues a short reminder prepended to the next user message (`takeReminder`, `role.go:106-115`) so the prompt cache stays intact.
- This is orthogonal to the other `offered` gates (read-only sessions, missing stores like `Memories`/`Know`/`Minion`) — role is just the first check.

## calls
search_code map[pattern:copilot]
search_code map[pattern:advisor]
read_file map[path:internal/agent/role.go]
read_file map[path:internal/agent/role.type.go]
search_code map[file_type:go path:internal/agent pattern:roles:\s*Role]
search_code map[file_type:go path:internal/agent pattern:allows\(]
read_file map[end_line:210 path:internal/agent/tools.go start_line:100]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793476031593170737"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:15-19 tools_files.go:100 tools_run.go:50 tools_teach.go:25 tools_delegate.go:43 tools.go:116-118 tools.go:140-160 role.go:83-86 tools.go:195-199 role.go:92-99 role.go:106-115"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:100-210" 338ef86a

