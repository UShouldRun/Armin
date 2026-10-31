How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 161580 prompt (137680 cached), 2191 completion, $0.00639, 10 rounds, 11 tools

## answer


A role is a `uint8` bitmask, and every tool spec carries a `roles` mask — enforcement happens in two places: the tool list offered to the model, and the call itself.

**The mask** (`internal/agent/role.type.go:16-21`)

- `RoleMinion = 0b001`, `RoleCopilot = 0b010`, `RoleAdvisor = 0b100`

**Matching rule** (`role.go:81-86`)

```go
func (id ToolID) allows(r Role) bool {
  roles := specs[id].roles
  return r == 0 || roles == 0 || roles & r != 0
}
```

A tool with `roles: 0` (the default) is offered to **every** role; only tools that set an explicit mask are restricted. So the interesting cases are exactly the five explicit masks in the tree:

| tool | `roles:` | effect |
|---|---|---|
| `run_program` | `RoleCopilot` (tools_run.go:50) | only copilot runs programs |
| `edit_file`, `write_file` | `RoleMinion \| RoleCopilot` (tools_files.go:100,136) | **advisor can read but not write files** |
| `delegate` | `RoleCopilot \| RoleAdvisor` (tools_delegate.go:42) | **minion can't delegate** (it *is* the delegatee) |
| `learn_explained` | `RoleAdvisor` (tools_teach.go:25) | teaching flow is advisor-only; tests confirm "copilot role can't use learn_explained" (tools_teach_test.go:81-83) |

**Enforcement is at two layers** (tools.go):

1. **Offering** — `offered()` (tools.go:116-133) filters `Definitions()`, which is built once per role and cached per `Loop` (tools.go:141-165). So the model only *sees* tools its role admits, which also keeps prompt-cache prefixes stable.
2. **Execution** — even if the model emits a call for a tool its role doesn't admit, `execute()` (tools.go:192-200) returns a structured error: `"the minion role can't use delegate; the user can switch roles with /copilot"` — so the model reads the denial and can tell the user to `/minion` / `/copilot` / `/advisor`.

**On switch** (`role.go:90-114`): `SetRole` changes the mask and queues a short reminder string that is prepended to the *next user message* — not a system-prompt rewrite — so everything before it stays in the server's prompt cache. The reminders (role.go:28-39) also tell the model its behavioral posture (e.g. advisor: "if code needs writing, say what, and leave it to the copilot").

**Belt-and-braces, inside tools** — the know tree adds role checks beyond the mask (tools_know.go): a minion's `know_write` is capped at "inferred" confidence (`minionLimits`, line 532), and `verified: true` is rejected for the minion ("only the copilot or the advisor verifies", line 706-707) while authorship stays the minion's. And a minion that closes a `requires` loop gets told to leave it to the copilot/advisor (`cycleFix`, line 206).

## calls
search_code map[pattern:minion|copilot|advisor]
read_file map[end_line:60 path:docs/design/restructure-plan.md start_line:15]
search_code map[file_type:go pattern:Role.*tool|tool.*[Mm]ask|allowedTools|roleTools|toolsFor]
search_code map[file_type:go path:internal/agent pattern:Role]
search_code map[file_type:go path:internal/agent pattern:RoleMinion|RoleCopilot|RoleAdvisor|SetRole|func.*roles]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:\.roles|allows\(]
search_code map[file_type:go path:internal/agent pattern:RoleMinion\s|= 1\s*<<|Role Copilot|Role =|Role uint8|const \(]
run_command map[args:[-rn roles: internal/agent] binary:grep]
read_file map[end_line:215 path:internal/agent/tools.go start_line:110]
search_code map[file_type:go path:internal/agent/tools_know.go pattern:verified|Role]

## tree after

