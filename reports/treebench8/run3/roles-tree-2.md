Can the advisor role call run_program? Where is that decided in the code?

right true, 22138 prompt (18656 cached), 744 completion, $0.00148, 3 rounds, 3 tools

## answer


No. `run_program` is declared with `roles: RoleCopilot` only, so the advisor can't call it — the error at tools.go:199 even tells the LLM "the %s role can't use %s".

Decided in two places:

1. **The spec** — `internal/agent/tools_run.go:48-50`:
   ```go
   id:    toolRunProgram,
   flags: flagWrite | flagRun | flagAgent,
   roles: RoleCopilot,
   ```
   This is the whole answer: only `RoleCopilot` (0b00000010) is in the bitmask.

2. **The enforcement** — `internal/agent/role.go:83-87`, `allows()`:
   ```go
   func (id ToolID) allows(r Role) bool {
     roles := specs[id].roles
     return r == 0 || roles == 0 || roles & r != 0
   }
   ```
   It's a bitmask AND: `RoleAdvisor` (0b00000100) & `RoleCopilot` (0b00000010) == 0, so it's denied. (A role of 0 or a spec with no roles set means "all".)

The check is applied when a tool is dispatched in `internal/agent/tools.go` — around line 197 it returns the "can't use" error if `allows(l.Role)` fails. The role bitmask itself is defined in `internal/agent/role.type.go:15-19`.

## calls
read_file map[path:internal/agent/role.type.go]
read_file map[end_line:70 path:internal/agent/tools_run.go start_line:30]
search_code map[path:internal/agent pattern:roles]

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

