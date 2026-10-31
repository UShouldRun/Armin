How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 81382 prompt (66512 cached), 1372 completion, $0.00364, 6 rounds, 10 tools

## answer


A role is a **tool permission mask**, enforced in two layers — offerting (what the model ever sees) and execution (a backstop if it calls something anyway).

**The mask itself**

- `Role` is a `uint8` bitmask in `internal/agent/role.type.go:16-21`: `RoleMinion = 0b001`, `RoleCopilot = 0b010`, `RoleAdvisor = 0b100`.
- Each tool spec declares which roles admit it via a `roles` field. `id.allows(r)` in `internal/agent/role.go:83-86` returns true if the spec has no `roles` (open to all) or the current role's bit is set.

**Which tools are restricted per role** (everything else — reads, search, git, web — is `roles: 0` and available to all):

| tool | `roles` mask |
|---|---|
| `edit_file`, `write_file` (`tools_files.go:100,136`) | minion \| copilot |
| `run_program` (`tools_run.go:50`) | copilot only |
| `delegate` (`tools_delegate.go:43`) | copilot \| advisor |
| `learn_explained` (`tools_teach.go:25`) | advisor only |

So in practice: **minion** = reads/searches + small file edits (no `run_program`, no `delegate` — it can't delegate down to another minion); **copilot** = every tool (`role_test.go:43-45` asserts it gets the full list); **advisor** = read-only on files (no `edit_file`/`write_file`/`run_program`) but keeps web, tests, and gets `delegate` + the teaching tool `learn_explained`.

**Enforcement**

1. **Offerting** — `Loop.offered(id)` (`tools.go:116-119`) filters by `id.allows(l.Role)` (plus other capability flags like `ReadOnly`, `Know != nil`). The per-role list is built once in `defsFor()` (`tools.go:152-176`) and that's all the model sees, so an advisor never even sees `run_program` in its tool list.
2. **Runtime check** — `Loop.execute` (`tools.go:195-199`) re-checks `offered`; if the model hallucinates or calls a stale tool, nothing runs and it gets a readable error: `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

**Extras**

- One role at a time; role 0 ("none") admits every tool. Switching is `SetRole` (`role.go:91-97`) — it queues a reminder prepended to the next user message (not the system prompt) so the prompt cache survives.
- When the advisor delegates, its minion is forced read-only: `edits := in.Edits && l.Role != RoleAdvisor` (`tools_delegate.go:122-123`).
- Pinned by tests: `role_test.go` (masks + refuse-at-execute), and golden files `definitions.{minion,copilot,advisor}.json` regenerated with `-update` (`definitions_test.go:54-60`).

## calls
search_code map[pattern:minion]
search_code map[pattern:copilot|advisor]
search_code map[file_type:go path:internal/agent pattern:RoleMinion|RoleCopilot|RoleAdvisor]
find_files map[path:internal/agent pattern:role*.go]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:\s*Role]
search_code map[file_type:go path:internal/agent pattern:allows\(|role can't use|Definitions\(\)]
read_file map[end_line:210 path:internal/agent/tools.go start_line:100]
read_file map[path:internal/agent/role_test.go]

## tree after

