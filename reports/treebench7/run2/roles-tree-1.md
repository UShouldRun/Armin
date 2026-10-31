How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 90156 prompt (77520 cached), 1420 completion, $0.00382, 7 rounds, 8 tools

## answer


Roles are enforced in two layers, both keyed off a `roles` bitmask on each tool spec:

**1. Offer-time (what the model even sees)** — `Loop.offered()` is the gate: `internal/agent/tools.go:117` returns false when `!id.allows(l.Role)`. `Definitions()` (tools.go:142) filters the full spec table through `offered` and builds the tool list sent to the model, once per role. So a restricted tool is simply never in the model's tool list.

**2. Execute-time (defense in depth)** — `execute()` (tools.go:195) re-checks `offered`/`allows` when a call arrives. If the model calls a tool its role doesn't admit, it gets a structured error like `the advisor role can't use write_file; the user can switch roles with /copilot` (tools.go:196-201) instead of running it.

The per-role rules come from the `roles` field on each spec (a spec with no `roles` admits every role — `allows` at role.go:83-86):

| Tool | Roles | File |
|---|---|---|
| `run_program` | copilot only | tools_run.go:50 |
| `edit_file`, `write_file` | minion + copilot | tools_files.go:100, 136 |
| `delegate` | copilot + advisor (only offered when a minion is configured, via `flagMinion` at tools.go:133) | tools_delegate.go:43 |
| `learn_explained` | advisor only | tools_teach.go:25 |
| everything else (reads, git, know, scratchpad, `run_tests`, `build_check`, …) | all roles (no `roles` field) | — |

Net effect:
- **minion** — everything except `run_program` (and except `learn_explained`, which is advisor-only).
- **copilot** — everything (`role_test.go:46-48` asserts it gets the full list).
- **advisor** — everything except the write tools `edit_file`, `write_file`, `run_program`, but does get `delegate` (to hand writing work to the minion, whose edits are also approved by the user) and `learn_explained`.

The enforcement is confirmed by `TestRoleSetsToolList` and `TestRoleRefusesAtExecute` in `internal/agent/role_test.go:22-60`.

Note two adjacent limits that aren't role-based: `ReadOnly` sessions drop all `flagWrite` tools (tools.go:119), and feature tools are gated on what the Loop has configured (`flagMemories`, `flagSkills`, `flagKnow`, etc., tools.go:121-136). Also, switching roles doesn't rewrite the system prompt — `SetRole`/`takeReminder` (role.go:94-114) just prepends a short role reminder to the next user message, so the prompt cache stays intact.

## calls
search_code map[pattern:minion|copilot|advisor]
search_code map[file_type:go path:internal pattern:run_program|edit_file.*write_file|toolsFor|roleTools|forbidden]
search_code map[file_type:go path:internal/agent pattern:RoleMinion|RoleCopilot|RoleAdvisor]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:\.roles|flagWrite|hasRole|allowed]
find_references map[name:allows path:internal/agent]
read_file map[end_line:210 path:internal/agent/tools.go start_line:110]
search_code map[file_type:go path:internal/agent pattern:roles: Role]

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

