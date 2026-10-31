Can the advisor role call run_program? Where is that decided in the code?

right true, 39314 prompt (33024 cached), 965 completion, $0.00332, 4 rounds, 6 tools

## answer











No. `run_program` is restricted to the **copilot** role, so the advisor can't use it.

Where it's decided:

1. **The spec** — `internal/agent/tools_run.go:47-49`: the `toolRunProgram` entry declares `roles: RoleCopilot` (plus `flagWrite | flagRun | flagAgent`). The role constants are in `internal/agent/role.type.go:17-21` — `RoleCopilot = 0b00000010`, `RoleAdvisor = 0b00000100`, so the bitmask doesn't include advisor.

2. **The check** — `ToolID.allows` at `internal/agent/role.go:83-86`: `r == 0 || roles == 0 || roles & r != 0`. A spec with `roles` set admits only those roles; `RoleAdvisor & RoleCopilot == 0`, so it fails.

3. **Where it's enforced** — `Loop.offered` at `internal/agent/tools.go:116-118` (`case !id.allows(l.Role): return false`). That one gate does two jobs:
   - `defsFor` (tools.go:144-149) only puts offered tools into the role's definition list, so an advisor-Loop never even shows `run_program` to the model.
   - `execute` (tools.go:194-201) re-checks `offered` if the model calls it anyway, and returns the error *"the advisor role can't use run_program; the user can switch roles with /copilot"*.

Note it would also be blocked twice over: `run_program` carries `flagWrite` (tools.go:93-95), so a read-only Loop would refuse it regardless of role.

## calls
read_file map[end_line:230 path:internal/agent/tools.go start_line:90]
read_file map[path:internal/agent/role.type.go]
search_code map[path:internal/agent pattern:func \(r Role\) allows|allows\(]
search_code map[path:internal/agent pattern:toolRunProgram|run_program]
read_file map[end_line:110 path:internal/agent/tools_run.go start_line:40]
read_file map[end_line:100 path:internal/agent/role.go start_line:75]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474539444053118"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:17-21 internal/agent/role.go:83-86 tools_run.go:50 tools_files.go:100 tools_delegate.go:43 tools_teach.go:25 internal/agent/tools.go:116 tools.go:149 tools.go:194-204 tools.go:77-90 tools_delegate.go:123 docs/guide/config.md:58-62"
  evidence session "/home/henrique/Code/armin/internal/agent/role.go" 00000000
  evidence session "/home/henrique/Code/armin/internal/agent/role.type.go" 00000000
  evidence file "/home/henrique/Code/armin/internal/agent/tools.go:90-230" 5549be4b

