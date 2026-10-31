Can the advisor role call run_program? Where is that decided in the code?

right true, 77816 prompt (45184 cached), 855 completion, $0.00642, 6 rounds, 8 tools

## answer


Let me verify against the actual code:











**No — the advisor cannot call `run_program`.** (It's also not in the minion's list; only the copilot has it.) Decided in three places:

1. **The tool spec** — `internal/agent/tools_run.go:48-50` declares the restriction at the source:
   ```go
   {
     id:    toolRunProgram,
     flags: flagWrite | flagRun | flagAgent,
     roles: RoleCopilot,          // ← only the copilot role
   ```
   Roles are a bitmask in `internal/agent/role.type.go:15-19` (`RoleMinion=1`, `RoleCopilot=2`, `RoleAdvisor=4`).

2. **Offerting** — a tool is only sent to the model if its mask admits the role: `Loop.offered` → `id.allows(l.Role)` in `internal/agent/tools.go:116-119`, with `allows` defined at `internal/agent/role.go:83-86` (`roles & r != 0`). So the advisor never even sees `run_program` in its tool list — confirmed by `testdata/definitions.advisor.json` containing no `run_program` entry.

3. **Runtime rejection** — if the model calls it anyway, `Loop.execute` in `internal/agent/tools.go:195-199` returns an error: `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

It's pinned by tests: `internal/agent/role_test.go:26-30` asserts the advisor is never offered `edit_file`, `write_file`, or `run_program`, and the golden `definitions.advisor.json` keeps the exact list fixed.

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
search_code map[path:internal/agent pattern:run_program]
read_file map[path:internal/agent/role.type.go]
read_file map[end_line:75 path:internal/agent/tools_run.go start_line:40]
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent pattern:can't use|allows\(]
search_code map[path:internal/agent/testdata/definitions.advisor.json pattern:run_program]

## tree after

