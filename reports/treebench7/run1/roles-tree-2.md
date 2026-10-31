Can the advisor role call run_program? Where is that decided in the code?

right true, 27046 prompt (16192 cached), 573 completion, $0.00219, 3 rounds, 4 tools

## answer








No. Where it's decided:

- **The declaration:** `run_program`'s tool spec names only the copilot — `internal/agent/tools_run.go:48-51` (`roles: RoleCopilot`). Roles are bitmasks (`RoleMinion|RoleCopilot|RoleAdvisor`, `internal/agent/role.type.go:14-21`), so anything not listed in the mask is off-limits.
- **The enforcement:** `allows` in `internal/agent/role.go` checks `roles & r != 0` for each tool spec, both when building the tool list the model is offered and at execute time. Since `RoleCopilot` doesn't contain the `RoleAdvisor` bit, an advisor is never offered `run_program` and a stray call comes back with a "can't use" error (same path `role_test.go:63` exercises with `write_file`).
- **Intent, documented:** the advisor reminder says it can read "the code, the web and the library, but not change files" (`internal/agent/role.go:36-39`), and `role_test.go:26-30` asserts `run_program` isn't in the advisor's offered set. The only escape hatch is `delegate`: the advisor can hand a task to its minion, but there too edits are disabled (`tools_delegate.go:122-123`).

So the advisor reads and searches freely, but program execution is copilot-only, and the single point of truth for it is that one `roles:` line in the spec.

## calls
read_file map[path:internal/agent/role.type.go]
search_code map[path:internal/agent pattern:advisor]
search_code map[path:internal/agent/tools_run.go pattern:Role]
read_file map[path:internal/agent/role.go]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 armin session "bench-1793474423249455118"

n1 sessions topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 sessions/how-do-armin's-roles file checked a1 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.type.go:14-21 internal/agent/tools_files.go:100 internal/agent/tools_run.go:50 internal/agent/tools_delegate.go:43 internal/agent/tools_teach.go:25 internal/agent/role.go:83 internal/agent/tools.go:116 tools.go:143-149 loop.type.go:109 role.type.go:26 tools.go:195-199 internal/repl/roles.go:16-45 role.go:91 role.go:28-40 tools_files.go:100"
  evidence file "docs/design/restructure-plan.md:1-70" 17ee9bb6
  evidence file "internal/agent/role.go:66-100" 1820d5d1
  evidence file "internal/agent/tools.go:100-215" d469d3e6

