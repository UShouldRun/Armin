How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 78126 prompt (43056 cached), 1263 completion, $0.00617, 7 rounds, 9 tools

## answer




Roles are enforced per-tool via a `roles` bitmask field on each tool spec, checked in two places:

**The mechanism**

- `Role` is a bitmask: `RoleMinion = 1`, `RoleCopilot = 2`, `RoleAdvisor = 4` (internal/agent/role.type.go:15-19).
- Each tool spec declares which roles admit it. `allows` (internal/agent/role.go:83) returns true if the tool has no `roles` set (open to all) or the current role's bit is set.
- Two enforcement points:
  1. **Offering** — `Loop.offered()` (internal/agent/tools.go:116) filters the list sent to the model each turn, so disallowed tools never appear in the schema. The list is built once per role (`defsFor`, tools.go:149).
  2. **Execution** — `execute()` (internal/agent/tools.go:194-203) re-checks `offered`/`allows` and returns the error `"the %s role can't use %s; the user can switch roles with /copilot"` if the model calls one anyway.

**Which roles block which tools** (only 4 tools set `roles`):

| Tool | `roles` | So... |
|---|---|---|
| `run_program` (tools_run.go:50) | copilot only | minion **and** advisor can't run code |
| `edit_file`, `write_file` (tools_files.go:100,136) | minion \| copilot | advisor can't change files |
| `learn_explained` (tools_teach.go:25) | advisor only | minion/copilot can't record explanations |
| `delegate` (tools_delegate.go:43) | copilot \| advisor | only if a minion is configured (`flagMinion`) |

Everything else (reads, search, web, memory, etc.) has `roles: 0` and is available to all three.

That gives the doc's summary (docs/guide/config.md:60-62): minion = everything except `run_program`; copilot = everything; advisor = no `edit_file`/`write_file`/`run_program`. Tests pin this: role_test.go:24-48 asserts the advisor lacks the three, the minion lacks `run_program` but keeps `edit_file`, and the copilot sees the full set.

Note the limitation is "not offered + refused at call time," not a sandbox: the model simply never sees the schema for disallowed tools, and a hallucinated call gets a readable error.

## calls
search_code map[pattern:minion|copilot|advisor]
list_languages map[]
find_files map[path:internal/agent pattern:role*.go]
search_code map[path:internal/agent pattern:Roles\b.*Role|RoleMinion|RoleCopilot|RoleAdvisor]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent pattern:allows\(|roleDefs\[]
search_code map[path:internal/agent pattern:roles: ]
read_file map[end_line:215 path:internal/agent/tools.go start_line:100]

## tree after

