Can the advisor role call run_program? Where is that decided in the code?

right true, 22865 prompt (19408 cached), 872 completion, $0.00166, 3 rounds, 3 tools

## answer


No — the advisor can't call `run_program`.

It's decided in two places:

1. **The declaration** — `internal/agent/tools_run.go:48-50`: the `run_program` spec has `roles: RoleCopilot` (and `flags: flagWrite | flagRun | flagAgent`). Roles are bitmasks defined in `internal/agent/role.type.go:15-19` (`RoleAdvisor = 0b00000100`, `RoleCopilot = 0b00000010`), and since `RoleCopilot & RoleAdvisor == 0`, the advisor isn't in the mask.

2. **The check** — `internal/agent/role.go:83-86`:

   ```go
   func (id ToolID) allows(r Role) bool {
     roles := specs[id].roles
     return r == 0 || roles == 0 || roles & r != 0
   }
   ```

   A tool with a zero `roles` field is offered to every role; `run_program`'s isn't, so the bit test fails. Enforcement happens at call time in `internal/agent/tools.go:197-201`, which returns `"the %s role can't use %s; the user can switch roles with /copilot"` — and the same mask filters the tool definitions sent to the model per role (built per-role in `role.type.go`'s `roleDefs`).

So the advisor gets `delegate` and `learn_explained` (its only `roles: RoleAdvisor` specs, `tools_delegate.go:43`, `tools_teach.go:25`) plus all role-agnostic tools, but not `run_program` — only the copilot role can.

## calls
read_file map[end_line:60 path:internal/agent/role.type.go start_line:1]
read_file map[end_line:80 path:internal/agent/tools_run.go start_line:1]
search_code map[file_type:go path:internal/agent pattern:roles]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793475801300534889"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:15-19 internal/agent/tools_run.go:50 internal/agent/tools_files.go:100 internal/agent/tools_delegate.go:43 internal/agent/tools_teach.go:25 internal/agent/tools.go:116-119 internal/agent/role.go:83-87 tools.go:120-138 internal/agent/tools.go:141-160 internal/agent/tools.go:194-201 internal/agent/role.go:92-98 role.go:31-41 role.go:101-114 internal/agent/role_test.go:26-30"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:100-215" d469d3e6

