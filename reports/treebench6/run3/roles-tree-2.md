Can the advisor role call run_program? Where is that decided in the code?

right true, 72323 prompt (63232 cached), 1099 completion, $0.00672, 6 rounds, 9 tools

## answer


No — the advisor role cannot call `run_program`. It's decided in one place per tool: the `roles` bitmask on the tool spec.

**Where it's set:**
- `internal/agent/tools_run.go:50` — the `run_program` spec declares `roles: RoleCopilot` only.
- `internal/agent/role.type.go:15-19` — the bitmask constants: `RoleMinion` (0b001), `RoleCopilot` (0b010), `RoleAdvisor` (0b100). So advisor's bit isn't in `run_program`'s mask.

**Where it's enforced:**
- `internal/agent/role.go:83-87` — `ToolID.allows(r)` returns `roles & r != 0`.
- `internal/agent/tools.go:116-118` — `Loop.offered(id)` first case is `!id.allows(l.Role)`, so the tool is never offered to the advisor (its definitions list simply omits it).
- `internal/agent/tools.go:193-199` — as a backstop, if the model calls a known-but-unoffered tool, `execute` returns `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

**Test coverage:** `internal/agent/role_test.go:24-28` asserts the advisor is not offered `edit_file`, `write_file`, or `run_program` (and `role_test.go:38-43` the minion is offered `edit_file` but not `run_program`).

So the decision lives declaratively in each tool's spec (`roles:` field), checked via the bitmask in `allows`, with the tool-definition list and the execute-time error as the two enforcement points. This matches the docs (`docs/guide/config.md:61-62`: "the advisor no file changes (`edit_file`, `write_file`, `run_program`)") and what's recorded in the tree [n2].

## calls
know_find map[query:advisor role tools]
search_code map[pattern:advisor]
search_code map[path:internal/agent pattern:run_program]
read_file map[path:internal/agent/role.type.go]
read_file map[end_line:70 path:internal/agent/tools_run.go start_line:40]
search_code map[path:internal/agent pattern:roles]
read_file map[end_line:210 path:internal/agent/tools.go start_line:180]
read_file map[end_line:79 path:internal/agent/role.go start_line:40]
search_code map[path:internal/agent pattern:func \(l \*Loop\) offered]

## tree after
# armin know v1
# edit claims, kinds, flags, paths and edges; add nodes with
# "new <path> <kind> <conf>" blocks; evidence lines are read-only
author a1 copilot model "bench" session "bench-1793473527245627183"
author a2 armin session "bench-1793473527245627183"
author a3 armin session "bench-1793473551237618589"

n1 agent topic inferred a1 born 2026-10-31 seen 2026-10-31

n2 agent/roles concept checked a1 born 2026-10-31 seen 2026-10-31
  claim "Roles (minion/copilot/advisor) are a Role bitmask; a tool spec's roles mask limits which roles may call it (roles==0 admits all; no role admits all). Only edit_file/write_file (minion+copilot), run_program (copilot), delegate (copilot+advisor), learn_explained (advisor) carry masks. Enforced at offer time via Loop.offered/Definitions and at execute time with a readable error."
  evidence file "internal/agent/role.type.go:15-19" a56f13bc
  evidence file "internal/agent/role.go:81-86" fe3c0a6b
  evidence session "internal/agent/tools.go:116-119,193-199" 00000000
  evidence file "internal/agent/tools_run.go:50" b5a11498
  evidence file "internal/agent/tools_delegate.go:42" 09853e54
  evidence file "internal/agent/tools_teach.go:25" 6bb3c060
  evidence session "internal/agent/role_test.go" 00000000

n3 sessions topic inferred a2 born 2026-10-31 seen 2026-10-31

n4 sessions/how-do-armin's-roles file checked a2 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/role.go internal/agent/role.type.go internal/agent/role_test.go internal/agent/tools.go:110-210"
  evidence session "internal/agent/role.go" 00000000
  evidence session "internal/agent/role.type.go" 00000000
  evidence session "internal/agent/role_test.go" 00000000
  evidence file "internal/agent/tools.go:110-210" 5bbeede2

n5 sessions/can-the-advisor-role file checked a3 born 2026-10-31 seen 2026-10-31
  claim "internal/agent/tools_run.go:50 internal/agent/role.type.go:15-19 internal/agent/role.go:83-87 internal/agent/tools.go:116-118 internal/agent/tools.go:193-199 internal/agent/role_test.go:24-28 role_test.go:38-43 docs/guide/config.md:61-62"
  evidence file "internal/agent/role.go:40-79" df75f770
  evidence session "internal/agent/role.type.go" 00000000
  evidence file "internal/agent/tools.go:180-210" 55bb2d19
  evidence file "internal/agent/tools_run.go:40-70" 472cb111

