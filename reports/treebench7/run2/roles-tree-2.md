Can the advisor role call run_program? Where is that decided in the code?

right true, 72259 prompt (50112 cached), 988 completion, $0.00436, 6 rounds, 7 tools

## answer














No — the advisor can't call `run_program`. It's decided in three layers, all keyed off one field:

1. **The declaration** — `internal/agent/tools_run.go:50`: the `toolRunProgram` spec has `roles: RoleCopilot` (plus `flagWrite | flagRun | flagAgent`). Since roles are bitmasks (`RoleAdvisor = 0b100`, `role.type.go:19`), only copilot is admitted.

2. **The check** — `ToolID.allows` at `internal/agent/role.go:77-82`: `r == 0 || roles == 0 || roles & r != 0`. For the advisor, `RoleCopilot & RoleAdvisor` is 0, so it's not admitted.

3. **Where it's enforced** — `internal/agent/tools.go`:
   - `offered` (`tools.go:121`): `!id.allows(l.Role)` → the tool never makes it into the tool list the model sees for that role (built in `defsFor`).
   - `execute` (`tools.go:179-186`): if the model calls a tool its role doesn't admit, it gets back `"the advisor role can't use run_program; the user can switch roles with /copilot"` instead of running it.

So it's blocked both at offer time and at execution time. The same `roles` field is what lets the minion use `edit_file`/`write_file` but not `run_program` (`tools_files.go:100,136` vs `tools_run.go:50`), and tools with `roles: 0` are available to every role.

`role_test.go:26-28` pins this down: it asserts the advisor is *not* offered `run_program` (nor the file writers).

## calls
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent pattern:run_program]
read_file map[end_line:215 path:internal/agent/tools.go start_line:60]
read_file map[end_line:70 path:internal/agent/tools_run.go start_line:40]
search_code map[path:internal/agent pattern:RoleAdvisor =]
search_code map[path:internal/agent pattern:RoleMinion]
search_code map[file_type:go path:internal/agent pattern:RoleCopilot Role]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474471070599911"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools.go:117 tools.go:142 tools.go:195 tools.go:196-201 role.go:83-86 tools_run.go:50 tools_files.go:100 tools.go:133 tools_delegate.go:43 tools_teach.go:25 role_test.go:46-48 internal/agent/role_test.go:22-60 tools.go:119 tools.go:121-136 role.go:94-114"
  evidence session "internal/agent/role.go" 00000000
  evidence file "internal/agent/tools.go:110-210" ebcefe62

