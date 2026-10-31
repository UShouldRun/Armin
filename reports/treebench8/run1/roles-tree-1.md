How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 59184 prompt (44848 cached), 1661 completion, $0.00362, 6 rounds, 8 tools

## answer


Roles gate tools in armin in two ways — what the model is *offered* and a runtime *rejection* if it calls something it wasn't offered.

**1. Roles are a bitmask, and each tool spec names the roles that may call it.**
- `Role` is defined in `internal/agent/role.type.go:15-19` (`RoleMinion`, `RoleCopilot`, `RoleAdvisor`, plus `RoleAll`).
- Each tool spec has a `roles` field (a bitmask), e.g. `roles: RoleCopilot` for `run_program` (`internal/agent/tools_run.go:50`), `roles: RoleMinion | RoleCopilot` for `edit_file`/`write_file` (`internal/agent/tools_files.go:100,136`), `roles: RoleCopilot | RoleAdvisor` for `delegate` (`internal/agent/tools_delegate.go:43`), and `roles: RoleAdvisor` for `learn_explained` (`internal/agent/tools_teach.go:25`). A spec with no roles (`roles == 0`) admits every role.

**2. Offering: the role's tool list is built once per role.**
- `Loop.offered(id)` (`internal/agent/tools.go:116-119`) first checks `id.allows(l.Role)` — defined in `internal/agent/role.go:83-87`: a tool is allowed if the current role is none (`r == 0`, legacy behavior: every tool), the spec has no roles (`roles == 0`), or the spec's mask includes the role (`roles & r != 0`). Beyond the role, `offered` also checks runtime dependencies (Scratch, Know, Minion, Learner, etc., `tools.go:120-138`).
- `Loop.definitions()/defsFor()` (`internal/agent/tools.go:141-160`) iterates all tool IDs and includes only `offered` ones, cached in `roleDefs` per role slot, so each role gets a fixed tool schema list sent with every request.

**3. Enforcement: a second check at execution.**
- `Loop.execute` re-checks on every call: if the name isn't in the offered list and it fails specifically on `id.allows(l.Role)`, the model gets a structured error — "the advisor role can't use run_program; the user can switch roles with /copilot" (`internal/agent/tools.go:194-201`) — instead of it running.

**4. How the model learns about a role switch.**
- `SetRole` (`internal/agent/role.go:92-98`) just sets `l.Role` and queues a per-role reminder (`roleReminders`, `role.go:31-41`); `takeReminder` (`role.go:101-114`) prepends it to the next user message — deliberately not a system-prompt rewrite, so the server's prompt cache stays warm. The reminders themselves shape behavior: minion is told to do small contained edits and say when a task is too big; advisor is told it can't change files and should defer writing to the copilot.

**5. Pinned by tests.**
- `internal/agent/role_test.go:26-30` asserts the advisor isn't offered `edit_file`, `write_file`, or `run_program`; the exact per-role tool lists are golden files `internal/agent/testdata/definitions.{minion,copilot,advisor}.json`, checked byte-for-byte by `TestDefinitionsGolden` in `internal/agent/definitions_test.go` (regenerated with `-update`).

**Net effect per role:** copilot gets everything (including `run_program` and `delegate`); minion gets reads/searches plus `edit_file`/`write_file` but not `run_program` or `delegate`; advisor gets reads, web, knowledge tools and `delegate`/`learn_explained`, but no file writes and no `run_program`. No role set (`Role = 0`) means no restriction at all — the pre-roles default.

## calls
web_search map[query:armin coding assistant roles minion copilot advisor tools]
search_code map[pattern:minion|copilot|advisor]
list_directory map[]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:]
search_code map[file_type:go path:internal/agent pattern:allows\(]
read_file map[end_line:215 path:internal/agent/tools.go start_line:100]

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

