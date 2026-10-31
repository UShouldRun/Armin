Can the advisor role call run_program? Where is that decided in the code?

right true, 60631 prompt (49344 cached), 836 completion, $0.00551, 5 rounds, 5 tools

## answer











**No.** `run_program` is restricted to the copilot role only — the advisor can't call it (nor can the minion).

Where that's decided:

1. **The tool spec declares its allowed roles** — `internal/agent/tools_run.go:50`:
   ```go
   id:    toolRunProgram,
   flags: flagWrite | flagRun | flagAgent,
   roles: RoleCopilot,
   ```
   Each role is a bit flag defined in `internal/agent/role.type.go:16-20` (`RoleMinion = 0b001`, `RoleCopilot = 0b010`, `RoleAdvisor = 0b100`).

2. **The check itself** — `ToolID.allows()` in `internal/agent/role.go:88-92`:
   ```go
   func (id ToolID) allows(r Role) bool {
     roles := specs[id].roles
     return r == 0 || roles == 0 || roles & r != 0
   }
   ```
   A spec with `roles == 0` admits every role; `run_program`'s mask is `RoleCopilot`, so `RoleAdvisor & RoleCopilot == 0` → denied.

3. **Enforcement in practice** is two-layered: the Loop builds `roleDefs` per role (`SetRole` → `slot()` at role.go:68-77), so the advisor is simply **never offered** `run_program` in its tool definitions, and `allows()` acts as the gate on calls. This is pinned by tests: `internal/repl/roles_test.go` (advisor not offered write tools), `internal/agent/role_test.go`, and the golden per-role tool lists in `internal/agent/testdata/definitions.advisor.json` (regenerated with `-update`, checked by `definitions_test.go`).

It's also reinforced in prose: the advisor's switch reminder (role.go:36-39) tells the model "not change files", and the docs state the same (`docs/guide/config.md:60-62`).

## calls
search_code map[pattern:advisor]
search_code map[file_type:go pattern:run_program]
search_code map[file_type:go pattern:advisor]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:RoleCopilot|roles:]

## tree after

