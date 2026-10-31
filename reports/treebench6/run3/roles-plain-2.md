Can the advisor role call run_program? Where is that decided in the code?

right true, 76478 prompt (54976 cached), 1244 completion, $0.00846, 6 rounds, 10 tools

## answer


**No — the advisor cannot call `run_program`**, and it's decided in two layers:

**1. The tool spec declares which roles may use it** — `internal/agent/tools_run.go:48-50`:

```go
{
  id:    toolRunProgram,
  flags: flagWrite | flagRun | flagAgent,
  roles: RoleCopilot,   // only the copilot
  ...
```

Roles are bitmasks (`internal/agent/role.type.go:16-20`: minion `0b0001`, copilot `0b0010`, advisor `0b0100`), so `roles: RoleCopilot` admits only the copilot.

**2. Enforcement, in two places:**

- **Offering**: a role's tool list is built through `Loop.offered` → `id.allows(l.Role)` (`internal/agent/tools.go:116-119`). `allows` (`internal/agent/role.go:83-86`) returns true only if the spec's `roles` mask includes the current role — or the spec has no roles at all. Since the advisor isn't in the mask, it never sees `run_program` in its tool list.
- **Runtime check**: if the model calls it anyway, `Loop.execute` rejects it (`internal/agent/tools.go:194-199`) with `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

**Pinned by tests**: `TestRoleOffersPerRole` in `internal/agent/role_test.go:26-30` asserts the advisor is not offered `edit_file`, `write_file`, or `run_program`, and `TestRoleRefusesAtExecute` (`role_test.go:55ff`) checks the execution-path refusal. The per-role definition lists are kept byte-stable by the golden `definitions.*.json` tests.

Note the minion also lacks `run_program` (its bit isn't in the mask), so the copilot is the only role with it — matching the documented design in `docs/guide/config.md:61-62`.

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
read_file map[end_line:80 path:internal/agent/tools_run.go start_line:1]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go pattern:RoleAdvisor|RoleMinion|RoleCopilot]
search_code map[file_type:go pattern:allows\(]
search_code map[file_type:go path:internal/agent pattern:Role (Role|uint|iota)|type Role]
read_file map[end_line:210 path:internal/agent/tools.go start_line:185]
search_code map[file_type:go path:internal/agent/tools_run.go pattern:roles:]
read_file map[path:internal/agent/role.type.go]

## tree after

