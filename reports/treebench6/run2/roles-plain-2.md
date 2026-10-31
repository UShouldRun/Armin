Can the advisor role call run_program? Where is that decided in the code?

right true, 74110 prompt (44416 cached), 951 completion, $0.00858, 6 rounds, 8 tools

## answer


No — the advisor cannot call `run_program`. The copilot is the only role that has it. It's decided in a few places:

**1. The tool spec declares who may use it** — `internal/agent/tools_run.go:48-50`:
```go
{
  id:    toolRunProgram,
  flags: flagWrite | flagRun | flagAgent,
  roles: RoleCopilot,   // only copilot
  ...
}
```
Roles are a bitmask defined in `internal/agent/role.type.go:15-19` (`RoleMinion`/`RoleCopilot`/`RoleAdvisor`).

**2. Offerting** — `internal/agent/tools.go:116-119` (`Loop.offered`) drops a tool from the list if `!id.allows(l.Role)`. The mask check is `ToolID.allows` in `internal/agent/role.go:58-63`: a spec with no roles admits every role; here only the copilot bit is set, so neither advisor nor minion ever see `run_program`.

**3. Runtime enforcement** — if the model calls it anyway, `Loop.execute` in `internal/agent/tools.go:193-199` rejects it with `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

**4. Pinned by tests** — `internal/agent/role_test.go:26-30` asserts the advisor isn't offered `edit_file`, `write_file` or `run_program` (and :38 that the minion isn't offered `run_program` either).

So it's a single declaration (`roles: RoleCopilot` in the spec) enforced twice: the tool list built per role, and the check in `execute` for direct calls.

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
search_code map[file_type:go path:internal/agent pattern:run_program]
read_file map[path:internal/agent/role.type.go]
read_file map[end_line:75 path:internal/agent/tools_run.go start_line:35]
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent/tools.go pattern:allows|execute]
read_file map[end_line:210 path:internal/agent/tools.go start_line:192]

## tree after

