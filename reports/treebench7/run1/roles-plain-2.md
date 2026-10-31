Can the advisor role call run_program? Where is that decided in the code?

right true, 59658 prompt (38656 cached), 874 completion, $0.00683, 5 rounds, 8 tools

## answer


Let me verify against the actual source.




**No — the advisor cannot call `run_program`.** It's decided in three places:

1. **The tool spec declares who may use it** — `internal/agent/tools_run.go:50`:
   ```go
   id:    toolRunProgram,
   flags: flagWrite | flagRun | flagAgent,
   roles: RoleCopilot,
   ```
   Only `RoleCopilot` is in the bitmask. Roles are defined as bitmasks in `internal/agent/role.type.go:17-19` (`RoleMinion = 0b001`, `RoleCopilot = 0b010`, `RoleAdvisor = 0b100`), so advisor & copilot == 0.

2. **Offerting — the advisor never even sees it** — `internal/agent/tools.go:116-119` (`Loop.offered`) returns false unless `id.allows(l.Role)` passes, and `allows` is at `internal/agent/role.go:83-86`: a spec with a `roles` mask is admitted only if the current role's bit is set (a spec with no roles admits everyone).

3. **Runtime enforcement — if the model calls it anyway** — `Loop.execute` at `internal/agent/tools.go:195-201` rejects it with an error the model can read:
   ```
   the advisor role can't use run_program; the user can switch roles with /copilot
   ```

The role switch itself (`SetRole`, `internal/agent/role.go:92-98`) just sets `l.Role` and queues a reminder message; it's the per-tool `roles` bitmask that carries the actual permission policy.

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
search_code map[file_type:go path:internal/agent pattern:RoleCopilot|roles:]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
read_file map[end_line:70 path:internal/agent/tools_run.go start_line:40]
read_file map[end_line:210 path:internal/agent/tools.go start_line:180]
search_code map[file_type:go path:internal/agent pattern:func (l \*Loop) offered|allows\(]

## tree after

