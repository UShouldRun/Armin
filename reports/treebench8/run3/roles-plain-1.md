How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 43759 prompt (32656 cached), 996 completion, $0.00238, 4 rounds, 4 tools

## answer


It works as a **per-tool permission mask** enforced in two places — the model only *sees* the tools its role allows, and any disallowed call is refused again at execution time.

**How it's defined**

- Roles are bitflags in `internal/agent/role.type.go`: `RoleMinion`, `RoleCopilot`, `RoleAdvisor` (`role.type.go:17-20`).
- Every tool registration declares a `roles:` field listing which roles may use it, e.g. `tools_files.go:100` gives `edit_file` `RoleMinion | RoleCopilot` (so advisor is excluded), and `tools_teach.go:25` gives `learn_explained` to `RoleAdvisor` only.
- `Loop.SetRole` (role.go:91) switches the active role and recomputes the tool list; the loop's `Role` field drives `allows()`, which checks the tool spec against the current role (`loop.type.go:104`).

**What each role gets** (per `docs/guide/config.md:60` and `role_test.go`):

| Role | Tool access |
|---|---|
| **minion** | Everything **except** `run_program`. Can `edit_file`/`write_file` (small edits) but only when the delegator allows them. Its knowledge-tree writes stay marked unverified and its confidence is capped. |
| **copilot** | The **full tool table**, plus `delegate` for handing tasks to a minion. |
| **advisor** | Read-only plus teaching tools: **no `edit_file`, `write_file`, or `run_program`** (role_test.go:26). Has `delegate` too, but the advisor's minion "never edits" (config.md:68-69). |

**Enforcement details**

- The mask is applied to the definitions sent to the model, per role, so the prompt cache stays stable — one golden file per role in `definitions_test.go:54-59`.
- `TestRoleRefusesAtExecute` (`role_test.go:55`) confirms a second layer: even if a model calls a tool its role doesn't allow, execution is refused.
- Role switches also inject a reminder message (`roleReminders`, role.go:28-38) telling the model what it can and can't do — e.g. the advisor is told "not change files; when code needs writing, say so".
- Role-specific behavior beyond tools: `know_write` limits differ (minion claims can't be marked verified, `tools_know_test.go:145`), and cycle-checking in `turn.go:227` skips the minion role.

So: minion = everything except running programs, copilot = everything, advisor = read/teach only (no file or program changes), with `delegate` as the bridge from the hosted roles down to the local minion.

## calls
search_code map[pattern:minion]
search_code map[pattern:advisor]
search_code map[file_type:go pattern:RoleMinion|RoleAdvisor|RoleCopilot]
search_code map[file_type:go pattern:SetRole|func.*Roles\(\)|\.Roles &]

## tree after

