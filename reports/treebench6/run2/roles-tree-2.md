Can the advisor role call run_program? Where is that decided in the code?

right true, 61709 prompt (54208 cached), 1040 completion, $0.00592, 6 rounds, 6 tools

## answer


No. `run_program` is offered to the copilot role only — the advisor's bitmask excludes it.

Where it's decided:

- **`internal/agent/tools_run.go:50-51`** — the tool spec for `run_program` declares `roles: RoleCopilot`.
- **`internal/agent/role.type.go:15-18`** — the role bitmasks: `RoleMinion = 0b001`, `RoleCopilot = 0b010`, `RoleAdvisor = 0b100`. The comment at lines 12-13 explains the `roles` field names every role that may call the tool.
- The role reminder text reinforces it (`internal/agent/role.go:36-38`): the advisor "can read the code, the web and the library, but not change files."
- **`internal/agent/role_test.go:24-28`** — the test asserts the advisor is *not* offered `edit_file`, `write_file`, or `run_program`.

So gating is per-tool via the `roles` bitmask on each spec; the Loop filters the tool definitions sent to the model by the current role. (Related: minion also can't call it — `role_test.go:38` — while `run_tests`/`build_check` remain available as the sanctioned alternatives.)

## calls
know_find map[query:advisor role run_program tool permissions]
read_file map[end_line:40 path:internal/agent/role.type.go start_line:1]
search_code map[path:internal/agent pattern:run_program]
search_code map[path:internal/agent pattern:RoleAdvisor|RoleCopilot|RoleMinion]
read_file map[end_line:75 path:internal/agent/tools_run.go start_line:40]
read_file map[end_line:51 path:internal/agent/tools_run.go start_line:46]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473478106548487"
author a2 armin session "bench-1793473478106548487"
author a3 armin session "bench-1793473512363403334"

n1 armin topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 armin/roles fact tested a1 born 2026-10-31 seen 2026-10-31
  claim "A Role (minion/copilot/advisor, bitmask at agent/role.type.go:17) gates which tools a Loop offers. Each tool spec names admitting roles via roles:; allows() (agent/role.go:83) admits tools with no roles for every role. Role-restricted tools: run_program=copilot only, edit_file/write_file=minion|copilot, delegate=copilot|advisor, learn_explained=advisor only. Unlisted tools go to all roles."
  evidence file "internal/agent/role.type.go:17-20" 53b99ab0
  evidence file "internal/agent/role.go:83-87" a619b3a1
  evidence file "internal/agent/tools_run.go:50" b5a11498
  evidence session "internal/agent/tools_files.go:100,136" 00000000
  evidence file "internal/agent/tools_delegate.go:42" 09853e54
  evidence file "internal/agent/tools_teach.go:25" 6bb3c060

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-do-armin's-roles file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools.go:118 tools_run.go:50 tools_files.go:100 tools_delegate.go:42 tools_teach.go:25 internal/agent/tools.go:194-199 tools_know.go:530-545 role.go:91 role.go:101 definitions_test.go:54-56"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:100-160" 2ec3f4b9

n5 sessions/can-the-advisor-role file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools_run.go:50-51 internal/agent/role.type.go:15-18 internal/agent/role.go:36-38 internal/agent/role_test.go:24-28 role_test.go:38"
  evidence file "internal/agent/tools_run.go:40-75" 4a720adb
  evidence file "internal/agent/tools_run.go:46-51" 2c9634b8

