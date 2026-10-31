Can the advisor role call run_program? Where is that decided in the code?

right true, 63815 prompt (46912 cached), 715 completion, $0.00543, 5 rounds, 6 tools

## answer












**No** — the advisor cannot call `run_program`.

Where it's decided:

1. **The role mask per tool** — `internal/agent/tools_run.go:48-51`: `run_program`'s spec declares `roles: RoleCopilot`. Only the copilot bit is set (minion and advisor are not).

2. **The check itself** — `internal/agent/role.go:83-86`, `(ToolID).allows(r)`: a tool is admitted only if its `roles` bitmask includes the current role (`r == 0 || roles == 0 || roles & r != 0`). Role bit constants live in `internal/agent/role.type.go:16-19` (minion `0b001`, copilot `0b010`, advisor `0b100`).

3. **Where it's enforced** — `internal/agent/tools.go:116-118` (`offered`) filters what the model is even offered via `Definitions()`, and `tools.go:194-198` rejects a call if the role doesn't allow it (with an explicit "role can't use this tool" error).

So even if the model emitted a `run_program` call as advisor, it'd be refused at the dispatch gate in `tools.go`. Pinned by `role_test.go` / `definitions_test.go` (per-role golden `definitions.<role>.json`), and documented in `docs/guide/config.md:61-62`.

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
search_code map[file_type:go path:internal pattern:advisor]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:Roles|RoleCopilot]
search_code map[file_type:go path:internal/agent pattern:allows\(]

## tree after

