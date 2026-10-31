Can the advisor role call run_program? Where is that decided in the code?

right true, 59727 prompt (50048 cached), 811 completion, $0.00558, 5 rounds, 7 tools

## answer


No — the advisor cannot call `run_program`. It's decided in three layers:

**1. The tool spec** — `internal/agent/tools_run.go:45-47`: the `run_program` spec declares `roles: RoleCopilot`. Roles are a bitmask (`internal/agent/role.type.go:15-19`): `RoleMinion = 0b001`, `RoleCopilot = 0b010`, `RoleAdvisor = 0b100`, so only the copilot is admitted. (The minion also lacks it — copilot is the sole holder.)

**2. Offerting** — `internal/agent/tools.go:116-120`: `Loop.offered` returns false when `!id.allows(l.Role)`, and `allows` (`internal/agent/role.go:57-63`) is `r == 0 || roles == 0 || roles & r != 0`. So an advisor's tool list never even includes `run_program` in the request to the model.

**3. Runtime enforcement** — `internal/agent/tools.go:194-199`: in `Loop.execute`, if the model calls a name that exists but isn't offered for the current role, it gets a structured error instead of running it (told to switch roles), so a stray call can't actually execute.

**Pinned by tests** — `internal/agent/role_test.go:26-30` asserts the advisor is not offered `edit_file`, `write_file` or `run_program`; the golden `definitions.advisor.json` keeps the exact offered list byte-stable (regenerated with `-update`).

This matches the design intent in `docs/guide/config.md:60-62`: "the advisor no file changes (`edit_file`, `write_file`, `run_program`)."

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
search_code map[file_type:go path:internal/agent pattern:run_program]
read_file map[path:internal/agent/role.type.go]
read_file map[end_line:80 path:internal/agent/tools_run.go start_line:1]
read_file map[path:internal/agent/role.go]
search_code map[path:internal/agent/tools.go pattern:allows|execute]

## tree after

