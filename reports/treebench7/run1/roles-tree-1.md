How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 99961 prompt (79632 cached), 2144 completion, $0.00514, 7 rounds, 11 tools

## answer


A role is a tool permission mask, enforced in two places (filtering the tool *list* sent to the model, and rejecting *calls*). Here's how it works:

**1. Roles are a bitmask** — `internal/agent/role.type.go:14-21`
- `Role` is a `uint8`: `RoleMinion = 0b0001`, `RoleCopilot = 0b0010`, `RoleAdvisor = 0b0100`. A Loop with no role set (`r == 0`) sees every tool.

**2. Each tool spec declares which roles admit it** — the `roles:` field on specs, e.g.:
- `edit_file` / `write_file`: `RoleMinion | RoleCopilot` (`internal/agent/tools_files.go:100,136`)
- `run_program`: `RoleCopilot` only (`internal/agent/tools_run.go:50`)
- `delegate`: `RoleCopilot | RoleAdvisor` (`internal/agent/tools_delegate.go:43`)
- teaching tools: `RoleAdvisor` only (`internal/agent/tools_teach.go:25`)
- a spec with no roles admits every role.

**3. The check** — `ToolID.allows(r)` at `internal/agent/role.go:83`:
```go
return r == 0 || roles == 0 || roles & r != 0
```
i.e. no-role or unrestricted tool → allowed; otherwise the role bit must be set.

**4. Enforced at definition time** — `Loop.offered()` (`internal/agent/tools.go:116`) starts with `case !id.allows(l.Role): return false`. `Definitions()`/`defsFor()` (`tools.go:143-149`) builds the tool list once per role and caches it in `Loop.roleDefs [4]roleDefs` (`loop.type.go:109`, `role.type.go:26`), byte-stable per role (pinned by golden files in `internal/agent/testdata/definitions.<role>.json`, `definitions_test.go`). So the model is literally never offered tools its role can't use.

**5. Enforced at call time** — even if the model calls a tool it wasn't offered, `execute()` (`tools.go:195-199`) returns a structured error like `the advisor role can't use run_program; the user can switch roles with /copilot` instead of running it.

**6. Switching** — `/minion`, `/copilot`, `/advisor` commands (`internal/repl/roles.go:16-45`) call `Loop.SetRole()` (`role.go:91`), which stores the role and queues a short role-reminder message (the per-role texts in `roleReminders`, `role.go:28-40`) in front of the next user message — deliberately instead of rewriting the system prompt, so the server's prompt cache stays valid.

So concretely: the advisor can't call `run_program` or `edit_file`/`write_file` — decided by the `roles: RoleCopilot` field in `internal/agent/tools_run.go:50` (respectively `tools_files.go:100,136`); the minion can edit but can't run programs or delegate; only copilot and advisor can `delegate` a task to a minion. The mask is checked both when building the per-role tool definitions and when executing any call.

## calls
search_code map[pattern:minion]
list_directory map[]
search_code map[file_type:go pattern:role]
read_file map[end_line:70 path:docs/design/restructure-plan.md start_line:1]
search_code map[file_type:go path:internal/agent pattern:Role]
search_code map[file_type:go path:internal/agent pattern:roles:]
search_code map[file_type:go path:internal/agent pattern:allows\(]
read_file map[end_line:100 path:internal/agent/role.go start_line:66]
read_file map[end_line:215 path:internal/agent/tools.go start_line:100]
search_code map[file_type:go path:internal/agent pattern:roleDefs]
search_code map[file_type:go path:internal/agent pattern:golden]

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

