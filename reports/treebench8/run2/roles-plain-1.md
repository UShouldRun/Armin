How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 75094 prompt (64928 cached), 1365 completion, $0.00342, 7 rounds, 7 tools

## answer


Roles work via a bitmask on each tool spec, checked in two places. Here's the mechanism:

**How it works** (`internal/agent/role.go:80`):
- Each tool spec carries a `roles` bitmask. `id.allows(r)` returns true if the role is unset, the tool has no role restriction, or the mask includes the role:
  ```go
  func (id ToolID) allows(r Role) bool {
    roles := specs[id].roles
    return r == 0 || roles == 0 || roles & r != 0
  }
  ```
- `Loop.offered(id)` (`internal/agent/tools.go:116`) is the gate: it first rejects any tool where `!id.allows(l.Role)`, then applies other conditions (ReadOnly, missing stores, etc.).
- Enforcement happens at two levels:
  1. **Offering** — `Loop.definitionsFor()` (`tools.go:148`) builds the per-role tool list (cached per role), so the model's system prompt simply doesn't advertise tools the role can't use.
  2. **Execution** — `Loop.execute()` (`tools.go:194`) re-checks `l.offered(id)` at call time; a disallowed call returns a readable error like `"the minion role can't use run_program; the user can switch roles with /copilot"` (`tools.go:196-203`).

**Per-role restrictions** (the only role-restricted tools, all with explicit `roles:` fields):

| Tool | Roles | Effect |
|---|---|---|
| `edit_file` (`tools_files.go:100`) | Minion \| Copilot | Advisor can't edit |
| `write_file` (`tools_files.go:136`) | Minion \| Copilot | Advisor can't write files |
| `run_program` (`tools_run.go:50`) | Copilot only | Minion and Advisor can't run code |
| `delegate` (`tools_delegate.go:43`) | Copilot \| Advisor | Minion can't delegate (confirmed by `tools_delegate_test.go:169`); also needs `l.Minion` configured |
| `learn_explained` (`tools_teach.go:25`) | Advisor only | Teaching tool, advisor's signature capability |

Everything else (searches, reads, know_* tools, web, etc.) has `roles: 0` and is admitted by all roles.

**Resulting profiles:**
- **minion** — everything except `run_program` (and `delegate`); can still `edit_file`/`write_file` for small edits.
- **copilot** — everything, including `run_program` and `delegate`.
- **advisor** — everything read-only in spirit: no `edit_file`, no `write_file`, no `run_program`, but gets `delegate` (to hand code work to the minion) and the exclusive `learn_explained`.

Note the enforcement is purely on the role bit: `know_*` tools are shared by all roles but internally degrade the minion — e.g. `know_write` caps its confidence at inferred and refuses to verify nodes (`tools_know.go:546, 723`).

The test at `internal/agent/role_test.go:36` pins this behavior: minion has no `run_program` but does have `edit_file`.

## calls
search_code map[pattern:minion]
list_languages map[]
search_code map[file_type:go pattern:RoleMinion|"minion"]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:roles:\s*Role]
search_code map[file_type:go path:internal/agent pattern:\.allows\(]
read_file map[end_line:220 path:internal/agent/tools.go start_line:90]

## tree after

