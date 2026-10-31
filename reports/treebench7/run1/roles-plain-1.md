How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 64395 prompt (47984 cached), 1169 completion, $0.00310, 5 rounds, 8 tools

## answer


Roles are a **permission bitmask**, enforced in two layers — offerting (what the model ever sees) and execution (what it can actually call).

**The mechanism**

- `Role` is a bitmask in `internal/agent/role.type.go:13-19`: `RoleMinion`, `RoleCopilot`, `RoleAdvisor`. A `Loop` holds one role at a time (or zero = all tools, the pre-roles default).
- Each tool spec declares who may use it via its `roles` field. `ToolID.allows` in `internal/agent/role.go:83-86` admits the role if it has no `roles` at all, or its mask overlaps the current role: `r == 0 || roles == 0 || roles & r != 0`.
- **Offerting**: `Loop.offered` / `defsFor` (`internal/agent/tools.go:116-118`, `149-152`) filter the tool list per role, cached per `roleDefs` slot, so a disallowed tool never appears in the definitions sent to the model.
- **Runtime**: `Loop.execute` re-checks (`internal/agent/tools.go:195-199`); if the model calls a tool its role doesn't admit, it gets an error like "the advisor role can't use run_program; the user can switch roles with /copilot".

**Who is restricted from what** (the only role-restricted tools):

| tool | roles mask | so: minion | copilot | advisor |
|---|---|---|---|---|
| `run_program` (`tools_run.go:50`) | `RoleCopilot` only | ✗ | ✓ | ✗ |
| `edit_file` / `write_file` (`tools_files.go:100,136`) | `RoleMinion \| RoleCopilot` | ✓ | ✓ | ✗ (no file changes) |
| `delegate` (`tools_delegate.go:43`) | `RoleCopilot \| RoleAdvisor` | ✗ | ✓ | ✓ |
| `learn_explained` (`tools_teach.go:25`) | `RoleAdvisor` only | ✗ | ✗ | ✓ |

Every other tool has no `roles` restriction and is available to all three.

Net effect, matching `docs/guide/config.md:60-62`: the minion gets everything but `run_program`; the copilot gets everything; the advisor gets read-only plus the advisor-only teaching tool and `delegate` (whose minion, if invoked by the advisor, never edits). Role switching (`/minion` etc.) only queues a short reminder text in front of the next user message (`role.go:25-45`, `SetRole`), leaving the cached system prompt untouched. Pinned by `internal/agent/role_test.go` and the golden `definitions.<role>.json` files in `internal/agent/testdata/`.

## calls
search_code map[pattern:minion]
search_code map[pattern:copilot]
search_code map[pattern:advisor]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:]
search_code map[file_type:go path:internal/agent pattern:allows\(]
search_code map[file_type:go path:internal/agent pattern:roleDefs]

## tree after

