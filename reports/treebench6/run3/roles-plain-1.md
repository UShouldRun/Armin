How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 107665 prompt (96000 cached), 1639 completion, $0.00441, 8 rounds, 11 tools

## answer


A role is a **bitmask tool-permission mask**, enforced at two points.

**1. The mask itself** — `internal/agent/role.type.go:12-19`

```go
type Role uint8
const (
  RoleMinion  Role = 0b00000001
  RoleCopilot Role = 0b00000010
  RoleAdvisor Role = 0b00000100
)
```

Each tool spec declares which roles may call it via its `roles` field (a spec with `roles: 0` admits every role). Only six tools restrict roles at all:

| Tool | roles | File |
|---|---|---|
| `run_program` | copilot only | `internal/agent/tools_run.go:50` |
| `edit_file` | minion, copilot | `internal/agent/tools_files.go:100` |
| `write_file` | minion, copilot | `internal/agent/tools_files.go:136` |
| `learn_explained` | advisor only | `internal/agent/tools_teach.go:25` |
| `delegate` | copilot, advisor | `internal/agent/tools_delegate.go:42` |

So the effective limits are:
- **minion** — everything except `run_program` and `delegate` (it *is* the local model, so it can't delegate to itself and can't run arbitrary programs).
- **copilot** — everything (all 38 tools; `role_test.go:46-49` asserts it gets the full list).
- **advisor** — everything except `edit_file`, `write_file`, `run_program`; plus advisor-only `learn_explained` and `delegate`.

**2. Enforcement, in two layers** (so the mask can't be bypassed even if the model hallucinates a call):
- **Offering** — `Loop.defsFor()` (`internal/agent/tools.go:147-168`) builds one cached tool-definition list per role, including only tools where `id.allows(l.Role)` is true (`internal/agent/role.go:84-89`). The advisor never even sees `run_program` in its tool list.
- **Runtime check** — `Loop.execute` (`internal/agent/tools.go:195-201`): if a call comes in for a tool the role can't use, it's rejected with `"the advisor role can't use run_program; the user can switch roles with /copilot"` — no crash, the model just sees the error and recovers next round.

A few adjacent behaviors:
- Switching roles (`SetRole`, `internal/agent/role.go:95-101`) appends a short reminder to the next user message (e.g. "reads but doesn't write") rather than rewriting the system prompt, keeping the prompt cache warm.
- `delegate` is extra-gated: it's only *offered* when `l.Minion != nil` (`tools.go:132-133`), and the advisor's delegated minion is forced read-only (`tools_delegate.go:121-122`) — since the advisor can't write files, neither can its minion.
- `role_test.go:24-49` pins all of this: the advisor isn't offered `edit_file`/`write_file`/`run_program`, the minion can edit but not run, and the copilot gets every tool.

## calls
search_code map[pattern:minion]
search_code map[pattern:copilot|advisor]
find_files map[path:internal/agent pattern:role*.go]
search_code map[file_type:go path:internal/agent pattern:RoleMinion|RoleCopilot|RoleAdvisor]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:\s+Role]
read_file map[end_line:215 path:internal/agent/tools.go start_line:180]
search_code map[file_type:go path:internal/agent pattern:func (l \*Loop) offered|roleDefs\[|l\.RoleDefs|allDefs|defs :=]
read_file map[end_line:178 path:internal/agent/tools.go start_line:138]
read_file map[end_line:138 path:internal/agent/tools.go start_line:126]

## tree after

