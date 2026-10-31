Can the advisor role call run_program? Where is that decided in the code?

right true, 58194 prompt (43584 cached), 798 completion, $0.00597, 5 rounds, 7 tools

## answer


No — the advisor cannot call `run_program`. It's decided in two places:

**1. The tool spec declares which roles may use it** — `internal/agent/tools_run.go:48-50`:

```go
{
  id:    toolRunProgram,
  flags: flagWrite | flagRun | flagAgent,
  roles: RoleCopilot,
  ...
}
```

`roles: RoleCopilot` admits only the copilot. (Roles are a bitmask: `RoleMinion`/`RoleCopilot`/`RoleAdvisor` in `internal/agent/role.type.go:15-19`.)

**2. Enforcement in the execution path** (`internal/agent/tools.go`):
- **Offering** — `Loop.offered` at `tools.go:116-120` returns false when `!id.allows(l.Role)`, so the tool is never sent to the model in the advisor's tool list.
- **Runtime check** — `tools.go:194-199`: if a call arrives for a tool the role can't use, it's rejected with an error telling the user they can switch roles with `/copilot`.

**Pinned by tests** — `internal/agent/role_test.go:24-29` asserts the advisor is not offered `edit_file`, `write_file`, or `run_program`.

Side note: the minion is also excluded — only the copilot role has `run_program`. This matches the design doc (`docs/guide/config.md:61-62`: "the copilot everything, the advisor no file changes").

## calls
search_code map[pattern:advisor]
search_code map[pattern:run_program]
read_file map[path:internal/agent/role.type.go]
search_code map[file_type:go path:internal/agent pattern:run_program]
read_file map[end_line:70 path:internal/agent/tools_run.go start_line:30]
read_file map[path:internal/agent/role.go]
search_code map[file_type:go path:internal/agent/tools.go pattern:allows]

## tree after

