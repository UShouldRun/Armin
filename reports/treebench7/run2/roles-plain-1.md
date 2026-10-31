How do armin's roles (minion, copilot, advisor) limit which tools the model can call?

right true, 119878 prompt (104112 cached), 1592 completion, $0.00465, 9 rounds, 12 tools

## answer


How it works, in the code:

**Roles are a bitmask on the Loop.** `Role` is a `uint8` in internal/agent/role.type.go:16-21 — `RoleMinion = 0b1`, `RoleCopilot = 0b10`, `RoleAdvisor = 0b100`. A `Loop` holds one role at a time (`l.Role`), set by `/minion`, `/copilot`, `/advisor` (internal/repl/roles.go).

**Each tool spec declares which roles may call it.** The spec's `roles Role` field (internal/agent/tools.type.go:35) defaults to `0`, meaning "every role". The only tools that restrict roles are:

| Tool | Allowed roles | Where |
|---|---|---|
| `run_program` | copilot only | internal/agent/tools_run.go:50 |
| `edit_file` | minion, copilot | internal/agent/tools_files.go:100 |
| `write_file` | minion, copilot | internal/agent/tools_files.go:136 |
| `delegate` | copilot, advisor | internal/agent/tools_delegate.go:43 |
| `learn_explained` | advisor only | internal/agent/tools_teach.go:25 |

Everything else (search, read, `run_tests`, `web_search`, …) is `roles: 0` and available to all three.

**Two enforcement points:**

1. *Offered*: `Loop.offered()` (internal/agent/tools.go:116) filters the tool definitions sent in the request via `id.allows(l.Role)` (internal/agent/role.go:83). The model simply never sees tools its role can't use.
2. *Execute*: if the model hallucinates or uses a stale definition, `execute` refuses with a readable error — e.g. `"advisor role can't use write_file"` (internal/agent/tools.go:196, tested in internal/agent/role_test.go:60).

So the net effect:
- **minion** — everything except `run_program` (but it *can* edit files; it's just a small local model expected to do small, contained edits).
- **copilot** — every tool.
- **advisor** — no file changes: no `edit_file`, `write_file`, or `run_program`; plus the advisor-only `learn_explained` teaching tool.

Two extra, subtler limits beyond the offer mask:
- **The advisor's delegated minion can't edit either** — `delegateRun` computes `edits := in.Edits && l.Role != RoleAdvisor` and runs the sub-Loop with `ReadOnly: !edits` (internal/agent/tools_delegate.go:123-142).
- **Knowledge-tree limits for the minion** — it can't mark claims `verified` ("only the copilot or the advisor verifies", internal/agent/tools_know.go:723), and its writes have their confidence capped at *inferred* (`minionLimits`, internal/agent/tools_know.go:546).

Also note a role switch doesn't rewrite the system prompt — a short reminder is prepended to the next user message (`takeReminder`, internal/agent/role.go:96) to keep the server's prompt cache warm.

## calls
project_manifest map[]
search_code map[pattern:minion]
search_code map[file_type:go path:internal/tools pattern:Role]
search_code map[file_type:go pattern:advisor]
search_code map[file_type:go path:internal/agent pattern:RoleCopilot|RoleMinion|RoleAdvisor]
find_files map[path:internal/agent pattern:role*.go]
read_file map[path:internal/agent/role.type.go]
read_file map[path:internal/agent/role.go]
read_file map[path:internal/agent/role_test.go]
search_code map[file_type:go path:internal/agent pattern:roles:\s*Role]
search_code map[file_type:go path:internal/agent pattern:allows\(|roles\s+Role]
search_code map[file_type:go path:internal/agent/tools_know.go pattern:Role]

## tree after

