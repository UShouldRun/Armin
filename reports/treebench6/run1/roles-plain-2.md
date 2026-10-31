Can the advisor role call run_program? Where is that decided in the code?

right true, 74888 prompt (58816 cached), 1132 completion, $0.00762, 6 rounds, 9 tools

## answer


No — the advisor cannot call `run_program`. It's decided in a few layers:

**1. The tool spec declares who may use it** — `internal/agent/tools_run.go:47-50`. The `run_program` spec has:
```go
id:    toolRunProgram,
flags: flagWrite | flagRun | flagAgent,
roles: RoleCopilot,   // only copilot is admitted
```
Roles are a bitmask defined in `internal/agent/role.type.go` (I can confirm the exact bit layout if you want, but the mask is what matters).

**2. Enforcement in the tool path** — two checks in `internal/agent/tools.go`:

- **Offerting** (`offered`, `tools.go:116-119`): before the tool list is sent to the model, `!id.allows(l.Role)` filters it out. `allows` is in `internal/agent/role.go:83-86` — a spec's role mask must include the current role (a zero mask admits all roles). So the advisor never even sees `run_program` in its tool list.
- **Runtime rejection** (`tools.go:192-199`): if the model emits the call anyway, `execute` returns the error `"the advisor role can't use run_program; the user can switch roles with /copilot"`.

**3. Pinned by tests** — `internal/agent/role_test.go:24-30` asserts the advisor is not offered `edit_file`, `write_file` or `run_program`; the minion similarly lacks `run_program` (`role_test.go:36-43`).

So the copilot is the only role with `run_program`, which matches the docs' "the minion has everything but `run_program`, the copilot everything, the advisor no file changes" (`docs/guide/config.md:61-62`).

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
search_code map[file_type:go path:internal/agent pattern:run_program]
find_files map[path:internal/agent pattern:role*.go]
read_file map[end_line:70 path:internal/agent/tools_run.go start_line:1]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent pattern:allows\(]
read_file map[end_line:130 path:internal/agent/tools.go start_line:110]
read_file map[end_line:210 path:internal/agent/tools.go start_line:185]

## tree after

